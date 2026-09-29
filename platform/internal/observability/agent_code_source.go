package observability

import (
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/grafana/sobek/ast"
)

const codePrefix = "async function diagnostic(){\n"

type codeReplacement struct {
	start int
	end   int
	value string
}

type codeRedactor struct {
	edits []codeReplacement
	names map[string]string
}

// redactCode retains only AST-sanctioned replacements and finite syntax gaps.
// Comments and unsupported spellings cause omission, never raw-source fallback.
func redactCode(source string, program *ast.Program) string {
	r := codeRedactor{names: make(map[string]string)}
	r.collect(reflect.ValueOf(program), 0)
	sort.Slice(r.edits, func(i, j int) bool {
		if r.edits[i].start == r.edits[j].start {
			return r.edits[i].end > r.edits[j].end
		}
		return r.edits[i].start < r.edits[j].start
	})
	var output strings.Builder
	position := 0
	for _, edit := range r.edits {
		if edit.end <= 0 || edit.start >= len(source) || edit.start < position {
			continue
		}
		if edit.start < 0 || edit.end > len(source) || edit.end < edit.start ||
			!safeCodeGap(source[position:edit.start]) {
			return ""
		}
		output.WriteString(source[position:edit.start])
		output.WriteString(edit.value)
		position = edit.end
	}
	if !safeCodeGap(source[position:]) {
		return ""
	}
	output.WriteString(source[position:])
	const maxSource = 1024
	if output.Len() > maxSource {
		return ""
	}
	return output.String()
}

func (r *codeRedactor) collect(value reflect.Value, depth int) {
	if !value.IsValid() || depth > 64 {
		return
	}
	if (value.Kind() == reflect.Pointer || value.Kind() == reflect.Interface) && value.IsNil() {
		return
	}
	if r.node(value) {
		return
	}
	switch {
	case value.Kind() == reflect.Interface || value.Kind() == reflect.Pointer:
		if !value.IsNil() {
			r.collect(value.Elem(), depth+1)
		}
	case value.Kind() == reflect.Slice:
		for i := range value.Len() {
			r.collect(value.Index(i), depth+1)
		}
	case value.Kind() == reflect.Struct:
		r.fields(value, depth)
	default:
	}
}

func (r *codeRedactor) fields(value reflect.Value, depth int) {
	if value.Type().PkgPath() != reflect.TypeFor[ast.Program]().PkgPath() {
		return
	}
	for i := range value.NumField() {
		if value.Type().Field(i).Name != "DeclarationList" {
			r.collect(value.Field(i), depth+1)
		}
	}
}

func (r *codeRedactor) node(value reflect.Value) bool {
	if value.Kind() == reflect.Struct && value.CanAddr() && value.Addr().CanInterface() {
		if node, ok := reflect.TypeAssert[ast.Node](value.Addr()); ok && r.add(node) {
			return true
		}
	}
	if value.CanInterface() {
		if node, ok := reflect.TypeAssert[ast.Node](value); ok {
			return r.add(node)
		}
	}
	return false
}

func (r *codeRedactor) add(node ast.Node) bool {
	replacement, found := r.replacement(node)
	if found {
		r.edits = append(r.edits, codeReplacement{int(node.Idx0()) - len(codePrefix) - 1,
			int(node.Idx1()) - len(codePrefix) - 1, replacement})
	}
	return found
}

func (r *codeRedactor) replacement(node ast.Node) (string, bool) {
	switch item := node.(type) {
	case *ast.Identifier:
		name := item.Name.String()
		allowed := diagnosticChoice(name, "input", "tools", "filter", "map", "sort", "reduce", "find", "some",
			"every", "flatMap", "slice", "includes", "join", "split", "$list", "$help", "Map", "Set", "Array",
			"Object", "JSON", "Promise", "Math", "orders", "workflow", "get", "list", "change", "catalog", "select")
		if allowed != diagnosticUnknown {
			return allowed, true
		}
		if r.names[name] == "" {
			r.names[name] = "v" + strconv.Itoa(len(r.names))
		}
		return r.names[name], true
	case *ast.StringLiteral, *ast.RegExpLiteral, *ast.TemplateLiteral:
		return `"[redacted]"`, true
	case *ast.NumberLiteral:
		return "0", true
	case *ast.BooleanLiteral:
		return "false", true
	case *ast.NullLiteral:
		return "null", true
	default:
		return "", false
	}
}

func safeCodeGap(gap string) bool {
	if strings.Contains(gap, "//") || strings.Contains(gap, "/*") {
		return false
	}
	for len(gap) > 0 {
		if strings.ContainsRune(" \t\r\n(){}[];:,.?+-*/%=!<>&|^~", rune(gap[0])) {
			gap = gap[1:]
			continue
		}
		end := 0
		for end < len(gap) && gap[end] >= 'a' && gap[end] <= 'z' {
			end++
		}
		if end == 0 || diagnosticChoice(gap[:end], "const", "let", "var", "return", "if", "else", "for", "of", "in",
			"while", "do", "break", "continue", "function", "async", "await", "new", "try", "catch", "finally",
			"throw", "switch", "case", "default", "typeof", "void", "delete", "instanceof") == diagnosticUnknown {
			return false
		}
		gap = gap[end:]
	}
	return true
}
