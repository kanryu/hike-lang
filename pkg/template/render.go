// Package template implements the compile-time template subset used by the
// Hike standard library and LLVM runtime generator.
package template

import (
	"fmt"
	"strings"
)

// Lookup resolves a statically known template field. Implementations should
// use a typed switch over the input structure; reflection is intentionally not
// part of this package.
type Lookup func(field string) (any, bool)

// RenderLiteral renders a literal template containing literal text and
// {{.Field}} actions. The caller is responsible for ensuring source came from
// a string literal or an embedded resource. No runtime parser is required by
// the generated program.
func RenderLiteral(source string, lookup Lookup) (string, error) {
	var out strings.Builder
	for pos := 0; pos < len(source); {
		start := strings.Index(source[pos:], "{{")
		if start < 0 {
			out.WriteString(source[pos:])
			break
		}
		start += pos
		out.WriteString(source[pos:start])
		end := strings.Index(source[start+2:], "}}")
		if end < 0 {
			return "", fmt.Errorf("template: unclosed action at byte %d", start)
		}
		end += start + 2
		action := strings.TrimSpace(source[start+2 : end])
		if action == "" || action[0] != '.' || strings.ContainsAny(action, " \t\r\n") {
			return "", fmt.Errorf("template: unsupported action %q", action)
		}
		field := action[1:]
		if field == "" || strings.Contains(field, ".") {
			return "", fmt.Errorf("template: invalid field action %q", action)
		}
		value, ok := lookup(field)
		if !ok {
			return "", fmt.Errorf("template: unknown field %q", field)
		}
		out.WriteString(fmt.Sprint(value))
		pos = end + 2
	}
	return out.String(), nil
}
