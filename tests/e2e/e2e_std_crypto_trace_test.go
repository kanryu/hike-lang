package e2e_test

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestE2EStd_CryptoTrace は、標準暗号実装をテストプログラムへコピーし、
// 各圧縮ラウンドの状態を出力する診断用テストです。通常の機能テストの
// ような固定出力比較ではなく、-v実行時のトレースをIR調査に使用します。
func TestE2EStd_CryptoTrace(t *testing.T) {
	t.Parallel()

	md5 := cryptoTraceSource(t, "md5", "Sum", "MD5", `
		printf("MD5 round=%d a=%d b=%d c=%d d=%d\n", i, a, b, c, d)
	`)
	sha := cryptoTraceSource(t, "sha256", "Sum256", "SHA", `
		printf("SHA round=%d a=%d b=%d c=%d d=%d e=%d f=%d g=%d z=%d\n", i, a, b, c, d, e, f, g, z)
	`)
	source := md5 + "\n" + sha + `

func printf(format string, ...) int

func main() int {
    md := CopiedMD5([]byte{97, 98, 99})
    sh := CopiedSHA([]byte{97, 98, 99})
    printf("MD5 result=%d,%d,%d,%d\n", md[0], md[1], md[2], md[3])
    printf("SHA result=%d,%d,%d,%d\n", sh[0], sh[1], sh[2], sh[3])
    return 0
}
`

	tmpDir, err := os.MkdirTemp(testCaseBase, "crypto-trace-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)
	srcPath := filepath.Join(tmpDir, "main.hike")
	if err := os.WriteFile(srcPath, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	stdRel, err := filepath.Rel(tmpDir, filepath.Join(projectRoot, "std"))
	if err != nil {
		t.Fatal(err)
	}
	mod := fmt.Sprintf("module crypto-trace\n\nhike 0.1.0\n\nreplace std => %s\n", filepath.ToSlash(stdRel))
	if err := os.WriteFile(filepath.Join(tmpDir, "hike.mod"), []byte(mod), 0644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(hikecBin, "run", srcPath)
	cmd.Dir = tmpDir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("crypto trace failed: %v\n%s", err, stderr.String())
	}
	t.Logf("crypto trace:\n%s", stdout.String())
}

// TestE2EStd_CryptoEmitIRComparison は、標準ライブラリ呼び出し版と、
// 同じ実装をコピーした版を同一モジュールへ入れてIRを生成します。
// -vで関数定義とメモリー命令の概要を確認できます。
func TestE2EStd_CryptoEmitIRComparison(t *testing.T) {
	md5 := strings.ReplaceAll(cryptoTraceSource(t, "md5", "Sum", "MD5", ""), "package main\n", "")
	sha := strings.ReplaceAll(cryptoTraceSource(t, "sha256", "Sum256", "SHA", ""), "package main\n", "")
	source := `package main

import "std/crypto/md5"
import "std/crypto/sha256"

` + md5 + "\n" + sha + `

func main() int {
    copiedMD5 := CopiedMD5([]byte{97, 98, 99})
    copiedSHA := CopiedSHA([]byte{97, 98, 99})
    standardMD5 := md5.Sum([]byte{97, 98, 99})
    standardSHA := sha256.Sum256([]byte{97, 98, 99})
    _ = copiedMD5; _ = copiedSHA; _ = standardMD5; _ = standardSHA
    return 0
}
`

	tmpDir, err := os.MkdirTemp(testCaseBase, "crypto-ir-")
	if err != nil {
		t.Fatal(err)
	}
	if os.Getenv("KEEP_CRYPTO_IR") == "" {
		defer os.RemoveAll(tmpDir)
	} else {
		t.Logf("crypto IR kept at: %s", tmpDir)
	}
	srcPath := filepath.Join(tmpDir, "main.hike")
	if err := os.WriteFile(srcPath, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	stdRel, err := filepath.Rel(tmpDir, filepath.Join(projectRoot, "std"))
	if err != nil {
		t.Fatal(err)
	}
	mod := fmt.Sprintf("module crypto-ir\n\nhike 0.1.0\n\nreplace std => %s\n", filepath.ToSlash(stdRel))
	if err := os.WriteFile(filepath.Join(tmpDir, "hike.mod"), []byte(mod), 0644); err != nil {
		t.Fatal(err)
	}

	outPath := filepath.Join(tmpDir, "crypto.ll")
	cmd := exec.Command(hikecBin, "emit-ir", "-o", outPath, srcPath)
	cmd.Dir = tmpDir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("emit-ir failed: %v\n%s", err, stderr.String())
	}
	ir, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(ir)
	bodies := make(map[string]string)
	for _, name := range []string{"md5_compress", "MD5_compress", "md5_Sum", "CopiedMD5", "sha256_compress", "SHA_compress", "sha256_Sum256", "CopiedSHA"} {
		definition := regexp.MustCompile(`(?m)^define .*@` + regexp.QuoteMeta(name) + `\(`).FindStringIndex(text)
		if definition == nil {
			t.Errorf("IR function not found: %s", name)
			continue
		}
		start := definition[0]
		end := strings.Index(text[start:], "\n}\n")
		if end < 0 {
			end = len(text) - start
		}
		body := text[start : start+end]
		bodies[name] = body
		t.Logf("%s: alloca=%d load=%d store=%d gep=%d add=%d\n", name,
			strings.Count(body, " alloca "), strings.Count(body, " load "),
			strings.Count(body, " store "), strings.Count(body, "getelementptr"),
			strings.Count(body, " add "))
	}
	for _, pair := range [][2]string{{"md5_compress", "MD5_compress"}, {"md5_Sum", "CopiedMD5"}, {"sha256_compress", "SHA_compress"}, {"sha256_Sum256", "CopiedSHA"}} {
		leftLines := strings.Split(normalizeCryptoIR(bodies[pair[0]]), "\n")
		rightLines := strings.Split(normalizeCryptoIR(bodies[pair[1]]), "\n")
		limit := len(leftLines)
		if len(rightLines) < limit {
			limit = len(rightLines)
		}
		for i := 0; i < limit; i++ {
			if leftLines[i] != rightLines[i] {
				t.Logf("%s/%s first IR difference at line %d:\n  std: %s\n  copy: %s", pair[0], pair[1], i+1, leftLines[i], rightLines[i])
				break
			}
		}
	}
}

func normalizeCryptoIR(s string) string {
	for _, prefix := range []string{"md5_", "MD5_", "sha256_", "SHA_"} {
		s = strings.ReplaceAll(s, prefix, "crypto_")
	}
	s = strings.ReplaceAll(s, "CopiedMD5", "crypto_Sum")
	s = strings.ReplaceAll(s, "CopiedSHA", "crypto_Sum256")
	s = regexp.MustCompile(`%[A-Za-z]+[0-9]+`).ReplaceAllString(s, "%reg")
	s = regexp.MustCompile(`for\.(cond|body|post|end)\.[0-9]+`).ReplaceAllString(s, "for.$1")
	return regexp.MustCompile(`if\.(then|else|end)\.[0-9]+`).ReplaceAllString(s, "if.$1")
}

func cryptoTraceSource(t *testing.T, dir, sumName, label, trace string) string {
	t.Helper()
	path := filepath.Join(projectRoot, "std", "crypto", dir, dir+".hike")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	s = strings.Replace(s, "package "+dir, "package main", 1)
	prefix := label + "_"
	symbols := []string{"Size", "BlockSize", "compress", sumName}
	if dir == "md5" {
		symbols = append(symbols, "leftRotate")
	} else {
		symbols = append(symbols, "rotr", "ch", "maj", "bigSigma0", "bigSigma1", "smallSigma0", "smallSigma1")
	}
	for _, symbol := range symbols {
		s = regexp.MustCompile(`\b`+regexp.QuoteMeta(symbol)+`\b`).ReplaceAllString(s, prefix+symbol)
	}
	if trace != "" {
		compressDecl := "func " + prefix + "compress("
		if pos := strings.Index(s, compressDecl); pos >= 0 {
			if brace := strings.Index(s[pos:], "{"); brace >= 0 {
				brace += pos
				traceLine := "\n\t" + blockTrace(label) + "\n"
				s = s[:brace+1] + traceLine + s[brace+1:]
			}
		}
	}
	s = strings.Replace(s, "func "+prefix+sumName+"(", "func Copied"+label+"(", 1)
	if dir == "md5" {
		s = strings.Replace(s, "\t\tb = b + "+prefix+"leftRotate(a+f+k[i]+m[g], r[i])\n", "\t\tb = b + "+prefix+"leftRotate(a+f+k[i]+m[g], r[i])\n"+trace, 1)
	} else {
		s = strings.Replace(s, "\t\ta = t1 + t2\n", "\t\ta = t1 + t2\n"+trace, 1)
	}
	return s
}

func blockTrace(label string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "printf(\"%s block=", label)
	for i := 0; i < 64; i++ {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString("%d")
	}
	b.WriteString("\\n\"")
	for i := 0; i < 64; i++ {
		fmt.Fprintf(&b, ", block[%d]", i)
	}
	b.WriteString(")")
	return b.String()
}
