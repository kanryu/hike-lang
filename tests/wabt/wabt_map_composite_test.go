package wabt_test

import "testing"

func TestWabtMapMissingSliceAppendThroughNode(t *testing.T) {
	wasm := buildWabt(t, `package main

import "std/maps"

func main() int {
	values := make(map[string][]int)
	values["numbers"] = append(values["numbers"], 10)
	values["numbers"] = append(values["numbers"], 20)
	return values["numbers"][0] + values["numbers"][1]
}`)
	if got, want := runWabt(t, wasm), "WABT_RESULT=30\n"; got != want {
		t.Fatalf("WABT output mismatch: got %q, want %q", got, want)
	}
}

func TestWabtGlobalMapLiteralIncrementThroughNode(t *testing.T) {
	wasm := buildWabt(t, `package main

import "std/maps"

var Metrics = map[string]int64{
    "requests_total": 0,
    "errors_total": 0,
    "bytes_sent": 0,
}

func main() int {
    Metrics["requests_total"]++
    Metrics["bytes_sent"] = Metrics["bytes_sent"] + 64
    return int(Metrics["requests_total"]*1000 + Metrics["errors_total"]*100 + Metrics["bytes_sent"])
}
`)
	if got, want := runWabt(t, wasm), "WABT_RESULT=1064\n"; got != want {
		t.Fatalf("WABT global map increment output = %q, want %q", got, want)
	}
}
