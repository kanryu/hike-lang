package wabt_test

import "testing"

func TestWabtGlobalInitializerOrderThroughNode(t *testing.T) {
	wasm := buildWabt(t, `package main

var gA int = 10
var gB int = gA * 2
var gC int = gB + 5

func main() int {
    return gA + gB + gC
}
`)
	if got, want := runWabt(t, wasm), "WABT_RESULT=55\n"; got != want {
		t.Fatalf("Wabt global initializer output = %q, want %q", got, want)
	}
}
