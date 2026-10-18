package main

import (
	"examples_go_hike_rust/fizzlib"
	"fmt"
	"unsafe"
)

func main() {
	const fileName = "サンプル.txt"

	if got := fizzlib.GetFizzFileSize(fileName); got != len(fileName)+100 {
		panic(fmt.Sprintf("GetFizzFileSize: got %d, want %d", got, len(fileName)+100))
	}
	if got := fizzlib.StringLength(fileName); got != len(fileName) {
		panic(fmt.Sprintf("StringLength: got %d, want %d", got, len(fileName)))
	}
	if got := fizzlib.EchoString(fileName); got != fileName {
		panic(fmt.Sprintf("EchoString: got %q, want %q", got, fileName))
	}
	if got := fizzlib.PrintString(fileName); got != len(fileName) {
		panic(fmt.Sprintf("PrintString: got %d, want %d", got, len(fileName)))
	}
	if got := fizzlib.MathScore(25); got != 50 {
		panic(fmt.Sprintf("MathScore: got %d, want 50", got))
	}
	const rawURL = "https://example.com:8443/path?q=hike"
	if got := fizzlib.UrlHost(rawURL); got != "example.com" {
		panic(fmt.Sprintf("UrlHost: got %q, want %q", got, "example.com"))
	}
	if got := fizzlib.UrlPort(rawURL); got != 8443 {
		panic(fmt.Sprintf("UrlPort: got %d, want 8443", got))
	}

	buf := make([]byte, 32)
	if got := fizzlib.ReadFizzFile(fileName, 12, unsafe.SliceData(buf)); got != 12 || string(buf[:12]) != "ABCDEFGHIJKL" {
		panic(fmt.Sprintf("ReadFizzFile: got %d/%q", got, string(buf[:12])))
	}

	var metaLen int
	if got := fizzlib.GetMetaData(fileName, &metaLen); got != "fizzbuzz" || metaLen != 8 {
		panic(fmt.Sprintf("GetMetaData: got %q/%d", got, metaLen))
	}

	fmt.Println("go-hike-rust validation: PASS")
}
