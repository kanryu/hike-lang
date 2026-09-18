package wabt_test

import "testing"

func TestWabtFuncCallMapDeleteLen(t *testing.T) {
	wasm := buildWabt(t, `package main

import "std/maps"

func main() int {
    m := make(map[string]int)
    m["alpha"] = 1
    m["beta"] = 2
    m["gamma"] = 3
    before := len(m)
    delete(m, "beta")
    after := len(m)
    return before*1000 + after*100 + m["alpha"]*10 + m["gamma"]
}
`)
	if got, want := runWabt(t, wasm), "WABT_RESULT=3213\n"; got != want {
		t.Fatalf("Wabt map delete result = %q, want %q", got, want)
	}
}

func TestWabtFuncCallMapLiteralInitialization(t *testing.T) {
	wasm := buildWabt(t, `package main

import "std/maps"

func main() int {
    var values = map[string]int{
        "alpha": 10,
        "beta": 20,
    }
    values["gamma"] = 30
    return len(values)*1000 + values["alpha"]*100 + values["beta"]*10 + values["gamma"]
}
`)
	if got, want := runWabt(t, wasm), "WABT_RESULT=4230\n"; got != want {
		t.Fatalf("Wabt map-literal result = %q, want %q", got, want)
	}
}

func TestWabtFuncCallVariadicStringSpreadAndForwarding(t *testing.T) {
	wasm := buildWabt(t, `package main

func concatAll(items ...string) string {
    res := ""
    for i := 0; i < len(items); i = i + 1 {
        res = res + items[i]
    }
    return res
}

func forward(first string, rest ...string) string {
    return first + "-" + concatAll(rest...)
}

func main() int {
    words := []string{"foo", "bar", "baz"}
    r1 := concatAll(words...)
    r2 := forward("START", words...)
    return len(r1)*100 + len(r2)
}
`)
	if got, want := runWabt(t, wasm), "WABT_RESULT=915\n"; got != want {
		t.Fatalf("Wabt variadic-string result = %q, want %q", got, want)
	}
}
