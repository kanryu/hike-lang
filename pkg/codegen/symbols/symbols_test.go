package symbols

import (
	"bytes"
	"encoding/json"
	"testing"

	"hikec-go/pkg/lexer"
	"hikec-go/pkg/parser"
	"hikec-go/pkg/sema"
)

func TestCollectAndWriteJSON(t *testing.T) {
	source := `package main

var globalValue int

type Counter struct {
    Value int
}

type Table struct {}

type Adder interface {
    Add(int) int
}

func (c *Counter) Add(value int) int {
    total := value
    return total
}

func (c *Counter) Other() int {
    return 0
}

func (t *Table) Get(index int) int {
    return index
}

func main() int {
    counter := Counter{}
    return counter.Add(1)
}
`

	program := parser.New(lexer.New(source)).ParseProgram()
	ctx, err := sema.Analyze(program)
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	got := Collect(program, ctx)

	if !contains(got.Functions, "main.main") || !contains(got.Globals, "globalValue") {
		t.Fatalf("functions/globals = %#v / %#v", got.Functions, got.Globals)
	}
	if len(got.TypeStructs) != 2 || got.TypeStructs[0].Name != "main.Counter" || !contains(got.TypeStructs[0].Members, "main.Counter.Value") {
		t.Fatalf("type structs = %#v", got.TypeStructs)
	}
	if len(got.TypeInterfaces) != 1 || got.TypeInterfaces[0].Name != "main.Adder" || !contains(got.TypeInterfaces[0].Methods, "main.Adder.Add") {
		t.Fatalf("type interfaces = %#v", got.TypeInterfaces)
	}
	if !contains(got.Locals, "counter") || !contains(got.Locals, "total") || !contains(got.Locals, "value") {
		t.Fatalf("locals = %#v", got.Locals)
	}
	if len(got.FixedReceivers) != 2 || got.FixedReceivers[0].Name != "main.Add" || got.FixedReceivers[0].Receiver != "*main.Counter" || got.FixedReceivers[1].Name != "main.Get" || got.FixedReceivers[1].Receiver != "*main.Table" {
		t.Fatalf("fixed receivers = %#v", got.FixedReceivers)
	}

	var output bytes.Buffer
	if err := WriteJSON(&output, program, ctx); err != nil {
		t.Fatalf("WriteJSON() error = %v", err)
	}
	var decoded Export
	if err := json.Unmarshal(output.Bytes(), &decoded); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
