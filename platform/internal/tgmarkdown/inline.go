package tgmarkdown

import (
	"strings"

	"github.com/yuin/goldmark/ast"
	extensionast "github.com/yuin/goldmark/extension/ast"
)

const strongEmphasisLevel = 2

type style struct {
	bold   bool
	italic bool
	strike bool
	noCode bool
	link   string
}

type run struct {
	text  string
	style style
	code  bool
}

func (c converter) inline(parent ast.Node, current style) (string, error) {
	runs := []run{}
	if err := c.collect(parent, current, &runs); err != nil {
		return "", err
	}
	return renderRuns(runs), nil
}

func (c converter) collect(parent ast.Node, current style, runs *[]run) error {
	for node := parent.FirstChild(); node != nil; node = node.NextSibling() {
		if err := c.collectNode(node, current, runs); err != nil {
			return err
		}
	}
	return nil
}

func (c converter) collectNode(node ast.Node, current style, runs *[]run) error {
	switch n := node.(type) {
	case *ast.Text:
		value := string(n.Value(c.source))
		if !n.IsRaw() {
			value = markdownText(n.Value(c.source))
		}
		if n.HardLineBreak() || n.SoftLineBreak() {
			value += "\n"
		}
		appendRun(runs, run{text: value, style: current})
	case *ast.String:
		value := string(n.Value)
		if !n.IsRaw() {
			value = markdownText(n.Value)
		}
		appendRun(runs, run{text: value, style: current})
	case *ast.Emphasis:
		if n.Level == strongEmphasisLevel {
			current.bold = true
		} else {
			current.italic = true
		}
		return c.collect(node, current, runs)
	case *extensionast.Strikethrough:
		current.strike = true
		return c.collect(node, current, runs)
	case *ast.CodeSpan:
		c.codeSpan(n, current, runs)
	case *ast.Link:
		target, err := linkTarget(n.Destination)
		if err != nil {
			return err
		}
		current.link = target
		return c.collect(node, current, runs)
	case *ast.Image:
		if current.link != "" {
			// A linked image is represented by its alt text and enclosing link.
			return c.collect(node, current, runs)
		}
		target, err := linkTarget(n.Destination)
		if err != nil {
			return err
		}
		current.link = target
		return c.collect(node, current, runs)
	case *ast.AutoLink:
		target, err := linkTarget(n.URL(c.source))
		if err != nil {
			return err
		}
		current.link = target
		appendRun(runs, run{text: markdownText(n.Label(c.source)), style: current})
	case *ast.RawHTML:
		appendRun(runs, run{text: string(n.Segments.Value(c.source)), style: current})
	default:
		return c.collect(node, current, runs)
	}
	return nil
}

func (c converter) codeSpan(node *ast.CodeSpan, current style, runs *[]run) {
	var out strings.Builder
	for child := node.FirstChild(); child != nil; child = child.NextSibling() {
		if value, ok := child.(*ast.Text); ok {
			out.WriteString(strings.ReplaceAll(string(value.Value(c.source)), "\n", " "))
		}
	}
	if current.link != "" || current.noCode {
		appendRun(runs, run{text: out.String(), style: current})
	} else {
		appendRun(runs, run{text: out.String(), code: true})
	}
}

func appendRun(runs *[]run, next run) {
	if next.text == "" {
		return
	}
	*runs = append(*runs, next)
}

type delimiter struct {
	open  string
	close string
}

func delimiters(value style) []delimiter {
	result := []delimiter{}
	if value.link != "" {
		result = append(result, delimiter{open: "[", close: "](" + value.link + ")"})
	}
	if value.bold {
		result = append(result, delimiter{open: "*", close: "*"})
	}
	if value.italic {
		result = append(result, delimiter{open: "_", close: "_"})
	}
	if value.strike {
		result = append(result, delimiter{open: "~", close: "~"})
	}
	return result
}

type inlineWriter struct {
	out           strings.Builder
	active        []delimiter
	lastDelimiter string
}

func renderRuns(runs []run) string {
	writer := inlineWriter{}
	for _, value := range runs {
		next := delimiters(value.style)
		if value.code {
			next = []delimiter{{open: "`", close: "`"}}
		}
		writer.transition(next)
		if value.code {
			writer.out.WriteString(escapeCharacters(value.text, "`\\"))
		} else {
			writer.out.WriteString(Escape(value.text))
		}
		writer.lastDelimiter = ""
	}
	writer.transition(nil)
	return writer.out.String()
}

func (w *inlineWriter) transition(next []delimiter) {
	shared := 0
	for shared < len(w.active) && shared < len(next) && w.active[shared] == next[shared] {
		shared++
	}
	for i := len(w.active) - 1; i >= shared; i-- {
		w.markup(w.active[i].close)
	}
	for _, value := range next[shared:] {
		w.markup(value.open)
	}
	w.active = next
}

func (w *inlineWriter) markup(value string) {
	// Telegram greedily reads __ as underline. An empty bold entity separates
	// neighboring italic delimiters without changing the visible text.
	if w.lastDelimiter == "_" && value == "_" {
		w.out.WriteString("**")
	}
	w.out.WriteString(value)
	w.lastDelimiter = value
}
