package main

import (
	"examples_gohike/fizzlib"
	"fmt"
)

func main() {
	fileName := "sample_target.txt"

	// 1. ファイルサイズの取得
	fileSize := fizzlib.GetFizzFileSize(fileName)
	fmt.Printf("[1] 取得サイズ: %d bytes\n", fileSize)

	// 2. バッファをGo側で確保して読み込み
	buf := make([]byte, 32)
	data := fizzlib.ReadFizzFile(fileName, fileSize, buf)
	fmt.Printf("[2] 読み込み結果 (len=%d): %s\n", len(data), string(data))

	// 3. C側でmallocされたメタデータを安全に取得（freeは自動完了）
	meta := fizzlib.GetMetaData(fileName)
	fmt.Printf("[3] メタデータ: %s\n", meta)
}
