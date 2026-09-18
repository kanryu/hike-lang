package diag

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var (
	diagFileRe *regexp.Regexp = regexp.MustCompile(`^(.*?):(\d+):(\d+):\s*(.*)$`)
	diagLineRe *regexp.Regexp = regexp.MustCompile(`^(?:line\s+)?(\d+):(\d+):\s*(.*)$`)
)

// Diagnostic は位置情報を含む単一のコンパイル時エラー・警告
type Diagnostic struct {
	Filename string
	Line     int
	Col      int
	Message  string
}

func (d Diagnostic) String() string {
	if d.Filename != "" && d.Line > 0 && d.Col > 0 {
		return fmt.Sprintf("%s:%d:%d: %s", d.Filename, d.Line, d.Col, d.Message)
	}
	if d.Filename != "" && d.Line > 0 {
		return fmt.Sprintf("%s:%d: %s", d.Filename, d.Line, d.Message)
	}
	if d.Filename != "" {
		return fmt.Sprintf("%s: %s", d.Filename, d.Message)
	}
	return d.Message
}

// Reporter はコンパイルエラーを蓄積し、Goコンパイラ互換の形式で管理する
type Reporter struct {
	errors []Diagnostic
}

func NewReporter() *Reporter {
	return &Reporter{errors: make([]Diagnostic, 0)}
}

func (r *Reporter) Errorf(filename string, line, col int, format string, args ...any) {
	r.errors = append(r.errors, Diagnostic{
		Filename: filename,
		Line:     line,
		Col:      col,
		Message:  fmt.Sprintf(format, args...),
	})
}

func (r *Reporter) Add(d Diagnostic) {
	r.errors = append(r.errors, d)
}

func (r *Reporter) AddRaw(defaultFile string, raw string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return
	}
	// Loader errors identify the actual source file before the diagnostic
	// lines. Preserve that file instead of attaching every line to the entry
	// source passed to the compiler phase.
	if strings.HasPrefix(raw, "parse error in ") {
		if idx := strings.Index(raw, ":\n"); idx >= 0 {
			file := strings.TrimPrefix(raw[:idx], "parse error in ")
			for _, line := range strings.Split(raw[idx+2:], "\n") {
				line = strings.TrimSpace(line)
				if line != "" {
					r.errors = append(r.errors, ParseDiagnostic(file, line))
				}
			}
			return
		}
	}
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			r.errors = append(r.errors, ParseDiagnostic(defaultFile, line))
		}
	}
}

func (r *Reporter) HasErrors() bool {
	return len(r.errors) > 0
}

func (r *Reporter) ErrorCount() int {
	return len(r.errors)
}

func (r *Reporter) Diagnostics() []Diagnostic {
	return r.errors
}

func (r *Reporter) Clear() {
	r.errors = r.errors[:0]
}

// FormatAll は行・列番号順に並べ替えてGoコンパイラ形式のテキストを生成
func (r *Reporter) FormatAll() string {
	if len(r.errors) == 0 {
		return ""
	}
	sorted := make([]Diagnostic, len(r.errors))
	for i := 0; i < len(r.errors); i++ {
		sorted[i] = r.errors[i]
	}
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].Filename != sorted[j].Filename {
			return sorted[i].Filename < sorted[j].Filename
		}
		if sorted[i].Line != sorted[j].Line {
			return sorted[i].Line < sorted[j].Line
		}
		return sorted[i].Col < sorted[j].Col
	})

	var sb strings.Builder
	for i, d := range sorted {
		if i > 0 {
			sb.WriteByte('\n')
		}
		sb.WriteString(d.String())
	}
	return sb.String()
}

func (r *Reporter) Error() string {
	return r.FormatAll()
}

// ParseDiagnostic はエラー文字列からファイル名、行、列、メッセージをパースしてDiagnostic構造体に変換する
func ParseDiagnostic(defaultFile, raw string) Diagnostic {
	raw = strings.TrimSpace(raw)

	// [Phase Error] 等の余分なプレフィックスを除去
	if strings.HasPrefix(raw, "[") && strings.Contains(raw, "]") {
		idx := strings.Index(raw, "]")
		raw = strings.TrimSpace(raw[idx+1:])
	}

	if m := diagFileRe.FindStringSubmatch(raw); len(m) == 5 {
		line, _ := strconv.Atoi(m[2])
		col, _ := strconv.Atoi(m[3])
		file := strings.TrimSpace(m[1])
		if file == "" {
			file = defaultFile
		}
		return Diagnostic{Filename: file, Line: line, Col: col, Message: strings.TrimSpace(m[4])}
	}

	if m := diagLineRe.FindStringSubmatch(raw); len(m) == 4 {
		line, _ := strconv.Atoi(m[1])
		col, _ := strconv.Atoi(m[2])
		return Diagnostic{Filename: defaultFile, Line: line, Col: col, Message: strings.TrimSpace(m[3])}
	}

	return Diagnostic{Filename: defaultFile, Line: 0, Col: 0, Message: raw}
}
