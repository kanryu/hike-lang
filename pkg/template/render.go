// Package template implements the compile-time template subset used by the
// Hike standard library and LLVM runtime generator.
package compiletemplate

import (
	"fmt"
	"strconv"
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
	return renderLiteralWithBlocks(source, lookup)
}

type templateNode interface {
	render(*strings.Builder, Lookup) error
}

type templateText string

func (n templateText) render(out *strings.Builder, _ Lookup) error {
	out.WriteString(string(n))
	return nil
}

type templateField struct{ name string }

func (n templateField) render(out *strings.Builder, lookup Lookup) error {
	value, ok := lookup(n.name)
	if !ok {
		return fmt.Errorf("template: unknown field %q", n.name)
	}
	out.WriteString(fmt.Sprint(value))
	return nil
}

type templateIf struct {
	field string
	want  string
	then  []templateNode
	else_ []templateNode
}

func (n templateIf) render(out *strings.Builder, lookup Lookup) error {
	value, ok := lookup(n.field)
	if !ok {
		return fmt.Errorf("template: unknown field %q", n.field)
	}
	part := n.else_
	if fmt.Sprint(value) == n.want {
		part = n.then
	}
	for _, child := range part {
		if err := child.render(out, lookup); err != nil {
			return err
		}
	}
	return nil
}

func renderLiteralWithBlocks(source string, lookup Lookup) (string, error) {
	nodes, pos, terminator, err := parseTemplateNodes(source, 0, false)
	if err != nil {
		return "", err
	}
	if terminator != "" || pos != len(source) {
		return "", fmt.Errorf("template: unexpected %q", terminator)
	}
	var out strings.Builder
	for _, node := range nodes {
		if err := node.render(&out, lookup); err != nil {
			return "", err
		}
	}
	return out.String(), nil
}

func parseTemplateNodes(source string, pos int, inBlock bool) ([]templateNode, int, string, error) {
	var nodes []templateNode
	for pos < len(source) {
		relStart := strings.Index(source[pos:], "{{")
		if relStart < 0 {
			nodes = append(nodes, templateText(source[pos:]))
			return nodes, len(source), "", nil
		}
		start := pos + relStart
		if start > pos {
			nodes = append(nodes, templateText(source[pos:start]))
		}
		relEnd := strings.Index(source[start+2:], "}}")
		if relEnd < 0 {
			return nil, 0, "", fmt.Errorf("template: unclosed action at byte %d", start)
		}
		end := start + 2 + relEnd
		action := strings.TrimSpace(source[start+2 : end])
		pos = end + 2
		switch {
		case action == "else" || action == "end":
			if !inBlock {
				return nil, 0, "", fmt.Errorf("template: unexpected %q", action)
			}
			return nodes, pos, action, nil
		case strings.HasPrefix(action, "if "):
			field, want, err := parseTemplateCondition(action)
			if err != nil {
				return nil, 0, "", err
			}
			thenPart, next, terminator, err := parseTemplateNodes(source, pos, true)
			if err != nil {
				return nil, 0, "", err
			}
			if terminator == "" {
				return nil, 0, "", fmt.Errorf("template: missing end for if at byte %d", start)
			}
			var elsePart []templateNode
			if terminator == "else" {
				elsePart, next, terminator, err = parseTemplateNodes(source, next, true)
				if err != nil {
					return nil, 0, "", err
				}
				if terminator != "end" {
					return nil, 0, "", fmt.Errorf("template: missing end for if at byte %d", start)
				}
			}
			nodes = append(nodes, templateIf{field: field, want: want, then: thenPart, else_: elsePart})
			pos = next
		case action == "" || action[0] != '.' || strings.ContainsAny(action, " \t\r\n"):
			return nil, 0, "", fmt.Errorf("template: unsupported action %q", action)
		default:
			field := action[1:]
			if field == "" || strings.Contains(field, ".") {
				return nil, 0, "", fmt.Errorf("template: invalid field action %q", action)
			}
			nodes = append(nodes, templateField{name: field})
		}
	}
	if inBlock {
		return nil, 0, "", fmt.Errorf("template: missing end for if")
	}
	return nodes, pos, "", nil
}

func parseTemplateCondition(action string) (string, string, error) {
	parts := strings.Fields(action)
	if len(parts) != 4 || parts[0] != "if" || parts[1] != "eq" || !strings.HasPrefix(parts[2], ".") {
		return "", "", fmt.Errorf("template: unsupported condition %q", action)
	}
	field := parts[2][1:]
	if field == "" || strings.Contains(field, ".") {
		return "", "", fmt.Errorf("template: invalid condition field %q", parts[2])
	}
	want, err := strconv.Unquote(parts[3])
	if err != nil {
		return "", "", fmt.Errorf("template: condition value must be a quoted string in %q", action)
	}
	return field, want, nil
}
