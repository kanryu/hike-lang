package wasm_test

import "testing"

func TestWasm32UnicodeAndURLMultibyte(t *testing.T) {
	wasm := buildWasm(t, `package main

import "std/net/url"
import "std/unicode/utf16"

func main() int {
    source := "Aあ😀"
    units := utf16.Encode(source)
    escaped := url.PathEscape("あ😀")
    return len(units)*100000 + len(escaped)
}
	`, "normal")
	if got, want := runWasm(t, wasm, "wasm_utf8.js"), "WASM_RESULT=400021\n"; got != want {
		t.Fatalf("WASM Unicode result = %q, want %q", got, want)
	}
}
