package loader

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"unicode"

	"hikec-go/pkg/target"
)

func defaultBuildTags() map[string]bool {
	tags := map[string]bool{runtime.GOOS: true, runtime.GOARCH: true}
	if runtime.GOOS == "linux" || runtime.GOOS == "darwin" || runtime.GOOS == "freebsd" || runtime.GOOS == "netbsd" || runtime.GOOS == "openbsd" {
		tags["unix"] = true
	}
	if runtime.GOOS != "js" && runtime.GOOS != "wasip1" {
		tags["cgo"] = true
	}
	return tags
}

// SetTarget enables Go-style GOOS/GOARCH/cgo tags for the compiler target.
func (l *Loader) SetTarget(tgt *target.Target) {
	tags := defaultBuildTags()
	if tgt == nil {
		l.buildTags = tags
		return
	}
	for k := range tags {
		delete(tags, k)
	}
	triple := strings.ToLower(tgt.Triple)
	goos := ""
	switch {
	case strings.Contains(triple, "windows"):
		goos = "windows"
	case strings.Contains(triple, "darwin"):
		goos = "darwin"
	case strings.Contains(triple, "linux"):
		goos = "linux"
	case strings.Contains(triple, "wasm"):
		goos = "wasip1"
	}
	goarch := ""
	switch {
	case strings.Contains(triple, "x86_64") || strings.Contains(triple, "amd64"):
		goarch = "amd64"
	case strings.Contains(triple, "arm64") || strings.Contains(triple, "aarch64"):
		goarch = "arm64"
	case strings.Contains(triple, "wasm64"):
		goarch = "wasm64"
	case strings.Contains(triple, "wasm32"):
		goarch = "wasm32"
	}
	if goos != "" {
		tags[goos] = true
	}
	if goarch != "" {
		tags[goarch] = true
	}
	if goos == "linux" || goos == "darwin" {
		tags["unix"] = true
	}
	if !tgt.IsWasm {
		tags["cgo"] = true
	}
	if tgt.Name != "" {
		tags[strings.ToLower(tgt.Name)] = true
	}
	l.buildTags = tags
}

func (l *Loader) fileAllowed(path string) bool {
	name := filepath.Base(path)
	if !filenameTagsMatch(name, l.buildTags) {
		return false
	}
	expr, ok := buildConstraint(path)
	if !ok {
		return true
	}
	return evalBuildExpr(expr, l.buildTags)
}

func filenameTagsMatch(name string, tags map[string]bool) bool {
	base := strings.TrimSuffix(name, filepath.Ext(name))
	parts := strings.Split(base, "_")
	if len(parts) < 2 {
		return true
	}
	last := parts[len(parts)-1]
	if last == "test" {
		parts = parts[:len(parts)-1]
		if len(parts) < 2 {
			return true
		}
		last = parts[len(parts)-1]
	}
	if isKnownOS(last) && !tags[last] {
		return false
	}
	if isKnownArch(last) && !tags[last] {
		return false
	}
	if len(parts) >= 2 {
		prev := parts[len(parts)-2]
		if isKnownOS(prev) && isKnownArch(last) {
			return tags[prev] && tags[last]
		}
		if isKnownArch(prev) && isKnownOS(last) {
			return tags[prev] && tags[last]
		}
	}
	return true
}

func isKnownOS(s string) bool {
	switch s {
	case "aix", "android", "darwin", "dragonfly", "freebsd", "hurd", "illumos", "ios", "js", "linux", "netbsd", "openbsd", "plan9", "solaris", "wasip1", "windows":
		return true
	}
	return false
}

func isKnownArch(s string) bool {
	switch s {
	case "386", "amd64", "arm", "arm64", "loong64", "mips", "mips64", "mips64le", "mipsle", "ppc64", "ppc64le", "riscv64", "s390x", "wasm32", "wasm64":
		return true
	}
	return false
}

func buildConstraint(path string) (string, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	return buildConstraintText(string(b))
}

func buildConstraintText(content string) (string, bool) {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if strings.HasPrefix(line, "package ") {
			break
		}
		if strings.HasPrefix(line, "//go:build ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "//go:build ")), true
		}
		if strings.HasPrefix(line, "//hike:build ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "//hike:build ")), true
		}
		if line != "" && !strings.HasPrefix(line, "//") {
			break
		}
	}
	return "", false
}

func validateInlineAsmBuildConstraint(path string, content []byte, tags map[string]bool) error {
	// Intrinsic names such as "llvm.x86.aesni.aesenc" may appear in ordinary
	// compiler code without any inline assembly.  Only source files that
	// actually contain Hike's inline-assembly form need architecture checks.
	if !strings.Contains(string(content), "__asm__") {
		return nil
	}
	text := strings.ToLower(string(content))
	feature := ""
	switch {
	case strings.Contains(text, "aesenc"), strings.Contains(text, "aesdec"), strings.Contains(text, "aesni"):
		feature = "amd64"
	case strings.Contains(text, "pclmul"):
		feature = "amd64"
	case strings.Contains(text, "rdrand"), strings.Contains(text, "rdseed"):
		feature = "amd64"
	case strings.Contains(text, "sha256rnds2"), strings.Contains(text, "sha256msg"):
		feature = "amd64"
	}
	if feature == "" {
		return nil
	}
	expr, ok := buildConstraintText(string(content))
	if !ok || !strings.Contains(expr, feature) {
		return fmt.Errorf("%s: inline assembly using %s requires a matching //go:build %s constraint", path, feature, feature)
	}
	if !tags[feature] {
		return fmt.Errorf("%s: inline assembly using %s is not supported by the selected target", path, feature)
	}
	return nil
}

type buildToken struct{ kind, value string }

func evalBuildExpr(expr string, tags map[string]bool) bool {
	tokens := tokenizeBuildExpr(expr)
	p := &buildParser{tokens: tokens, tags: tags}
	return p.parseOr()
}

func tokenizeBuildExpr(s string) []buildToken {
	var out []buildToken
	for i := 0; i < len(s); {
		if unicode.IsSpace(rune(s[i])) {
			i++
			continue
		}
		if strings.ContainsRune("()!", rune(s[i])) {
			out = append(out, buildToken{kind: string(s[i]), value: string(s[i])})
			i++
			continue
		}
		if strings.HasPrefix(s[i:], "&&") || strings.HasPrefix(s[i:], "||") {
			out = append(out, buildToken{kind: s[i : i+2], value: s[i : i+2]})
			i += 2
			continue
		}
		j := i
		for j < len(s) && (unicode.IsLetter(rune(s[j])) || unicode.IsDigit(rune(s[j])) || s[j] == '_') {
			j++
		}
		if j == i {
			i++
			continue
		}
		out = append(out, buildToken{kind: "tag", value: s[i:j]})
		i = j
	}
	return out
}

type buildParser struct {
	tokens []buildToken
	pos    int
	tags   map[string]bool
}

func (p *buildParser) peek(kind string) bool {
	return p.pos < len(p.tokens) && p.tokens[p.pos].kind == kind
}
func (p *buildParser) parseOr() bool {
	v := p.parseAnd()
	for p.peek("||") {
		p.pos++
		v = p.parseAnd() || v
	}
	return v
}
func (p *buildParser) parseAnd() bool {
	v := p.parseUnary()
	for p.peek("&&") {
		p.pos++
		v = p.parseUnary() && v
	}
	return v
}
func (p *buildParser) parseUnary() bool {
	if p.peek("!") {
		p.pos++
		return !p.parseUnary()
	}
	if p.peek("(") {
		p.pos++
		v := p.parseOr()
		if p.peek(")") {
			p.pos++
		}
		return v
	}
	if p.peek("tag") {
		v := p.tags[p.tokens[p.pos].value]
		p.pos++
		return v
	}
	if p.pos < len(p.tokens) {
		p.pos++
	}
	return false
}
