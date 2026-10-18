package main

import (
	"examples_gohike/fizzlib"
	"fmt"
	"unsafe"
)

func main() {
	const fileName = "サンプル.txt"

	// 1. Go string -> *byte + length の変換と整数戻り値を検証する。
	fileSize := fizzlib.GetFizzFileSize(fileName)
	if fileSize != len(fileName)+100 {
		panic(fmt.Sprintf("GetFizzFileSize: got %d, want %d", fileSize, len(fileName)+100))
	}
	fmt.Printf("[1] file size: %d bytes\n", fileSize)

	// C string引数がGo string APIとして公開され、C側でNUL終端まで
	// 正しく走査されたことを検証する（lenはUTF-8のバイト長）。
	if got := fizzlib.StringLength(fileName); got != len(fileName) {
		panic(fmt.Sprintf("StringLength: got %d, want %d", got, len(fileName)))
	}
	fmt.Printf("[1b] C string length: %d bytes\n", len(fileName))

	// C側でcharポインターを直接返し、Go側でstringへ戻せることを検証する。
	if echoed := fizzlib.EchoString(fileName); echoed != fileName {
		panic(fmt.Sprintf("EchoString: got %q, want %q", echoed, fileName))
	}
	fmt.Printf("[1c] direct char pointer: %q\n", fileName)

	// C側で受け取ったcharポインターが標準出力へ出力されることを検証する。
	if printed := fizzlib.PrintString(fileName); printed != len(fileName) {
		panic(fmt.Sprintf("PrintString: got %d, want %d", printed, len(fileName)))
	}
	fmt.Printf("[1d] C stdout length: %d bytes\n", len(fileName))

	// 2. ポインター引数、複数の整数引数、整数戻り値を検証する。
	buf := make([]byte, 32)
	want := 12
	written := fizzlib.ReadFizzFile(fileName, want, unsafe.SliceData(buf))
	if written != want || string(buf[:written]) != "ABCDEFGHIJKL" {
		panic(fmt.Sprintf("ReadFizzFile: got %d/%q", written, string(buf[:written])))
	}
	fmt.Printf("[2] read: %q\n", string(buf[:written]))

	// 3. *byte戻り値 -> Go string変換と out length 書き戻しを検証する。
	var metaLen int
	meta := fizzlib.GetMetaData(fileName, &metaLen)
	if meta != "fizzbuzz" || metaLen != len(meta) {
		panic(fmt.Sprintf("GetMetaData: got %q/%d", meta, metaLen))
	}
	fmt.Printf("[3] metadata: %q (len=%d)\n", meta, metaLen)
	fmt.Println("gohike validation: PASS")
}
