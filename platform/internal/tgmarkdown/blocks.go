package tgmarkdown

import (
	"strconv"
	"strings"

	"github.com/yuin/goldmark/ast"
)

type converter struct{ source []byte }

func (c converter) blocks(parent ast.Node, quoted bool) (string, error) {
	parts := []string{}
	for node := parent.FirstChild(); node != nil; node = node.NextSibling() {
		value, err := c.block(node, quoted)
		if err != nil {
			return "", err
		}
		parts = append(parts, value)
	}
	return strings.Join(parts, "\n\n"), nil
}

func (c converter) block(node ast.Node, quoted bool) (string, error) {
	switch n := node.(type) {
	case *ast.Paragraph, *ast.TextBlock:
		return c.inline(node, style{noCode: quoted})
	case *ast.Heading:
		return c.inline(node, style{bold: true, noCode: quoted})
	case *ast.FencedCodeBlock:
		if quoted {
			return Escape(strings.TrimRight(string(n.Lines().Value(c.source)), "\n")), nil
		}
		return fencedCode(string(n.Lines().Value(c.source)), string(n.Language(c.source))), nil
	case *ast.CodeBlock:
		if quoted {
			return Escape(strings.TrimRight(string(n.Lines().Value(c.source)), "\n")), nil
		}
		return fencedCode(string(n.Lines().Value(c.source)), ""), nil
	case *ast.Blockquote:
		value, err := c.blocks(node, true)
		if err != nil || quoted {
			return value, err
		}
		return ">" + strings.ReplaceAll(value, "\n", "\n>"), nil
	case *ast.List:
		return c.list(n, quoted)
	case *ast.ThematicBreak:
		return "────", nil
	case *ast.HTMLBlock:
		value := string(n.Lines().Value(c.source))
		if n.HasClosure() {
			value += string(n.ClosureLine.Value(c.source))
		}
		return Escape(strings.TrimRight(value, "\n")), nil
	default:
		return c.blocks(node, quoted)
	}
}

func (c converter) list(list *ast.List, quoted bool) (string, error) {
	items := []string{}
	number := list.Start
	for item := list.FirstChild(); item != nil; item = item.NextSibling() {
		value, err := c.blocks(item, quoted)
		if err != nil {
			return "", err
		}
		marker := "• "
		if list.IsOrdered() {
			marker = strconv.Itoa(number) + "\\. "
			number++
		}
		items = append(items, marker+strings.ReplaceAll(value, "\n", "\n  "))
	}
	return strings.Join(items, "\n"), nil
}

func fencedCode(value, language string) string {
	for _, r := range language {
		if r < 'a' || r > 'z' {
			if r < 'A' || r > 'Z' {
				if r < '0' || r > '9' {
					language = ""
					break
				}
			}
		}
	}
	if !strings.HasSuffix(value, "\n") {
		value += "\n"
	}
	return "```" + language + "\n" + escapeCharacters(value, "`\\") + "```"
}
