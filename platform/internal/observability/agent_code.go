package observability

import (
	"reflect"

	"github.com/grafana/sobek/ast"
	"github.com/grafana/sobek/parser"
)

const codeStructural = "structural"
const maxCodeNodes = 4096
const maxCodeOutline = 32

// CodeProfile contains structural evidence and, when supported, redacted code.
// Arbitrary identifiers, literal values and comments are never retained.
type CodeProfile struct {
	Status  string   `json:"status"`
	Bytes   int      `json:"bytes"`
	Nodes   int      `json:"nodes"`
	Outline []string `json:"outline,omitempty"`
	Methods []string `json:"methods,omitempty"`
	Source  string   `json:"source,omitempty"`
}

// ProfileAgentCode parses a bounded function body without evaluating it. Invalid
// and oversized source is omitted; parser errors and original source are never returned.
func ProfileAgentCode(code string) CodeProfile {
	const maxCode = 4096
	profile := CodeProfile{Status: "omitted", Bytes: len(code)}
	if len(code) == 0 || len(code) > maxCode {
		return profile
	}
	program, err := parser.ParseFile(nil, "", codePrefix+code+"\n}", 0, parser.WithDisableSourceMaps)
	if err != nil {
		return profile
	}
	profile.Status = codeStructural
	profile.walk(reflect.ValueOf(program), 0)
	if profile.Status == codeStructural {
		profile.Source = redactCode(code, program)
		if profile.Source != "" {
			profile.Status = "source_redacted"
		}
	}
	return profile
}

func (p *CodeProfile) walk(value reflect.Value, depth int) {
	const maxDepth = 64
	if !value.IsValid() || p.Status != codeStructural {
		return
	}
	if depth > maxDepth || p.Nodes >= maxCodeNodes {
		p.Status = "limited"
		return
	}
	switch {
	case value.Kind() == reflect.Interface || value.Kind() == reflect.Pointer:
		if !value.IsNil() {
			p.walk(value.Elem(), depth+1)
		}
	case value.Kind() == reflect.Slice:
		for i := range value.Len() {
			p.walk(value.Index(i), depth+1)
		}
	case value.Kind() == reflect.Struct:
		if value.Type().PkgPath() != reflect.TypeFor[ast.Program]().PkgPath() {
			return
		}
		p.Nodes++
		p.structure(value)
		for i := range value.NumField() {
			// DeclarationList duplicates AST subtrees; File contains source text.
			field := value.Type().Field(i)
			if field.Name != "DeclarationList" && field.Name != "File" {
				p.walk(value.Field(i), depth+1)
			}
		}
	default:
		// Scalars include every user-controlled spelling and literal value.
	}
}

func (p *CodeProfile) structure(value reflect.Value) {
	const maxOutline = 32
	name := value.Type().Name()
	var kind string
	switch name {
	case "ForStatement", "ForInStatement", "ForOfStatement", "WhileStatement", "DoWhileStatement":
		kind = "loop"
	case "FunctionLiteral", "ArrowFunctionLiteral":
		kind = "function"
	case "IfStatement", "SwitchStatement", "ConditionalExpression":
		kind = "branch"
	case "CallExpression":
		kind = "call"
	case "ReturnStatement":
		kind = "return"
	case "AwaitExpression":
		kind = "await"
	case "AssignExpression":
		kind = "assignment"
	}
	if kind != "" && len(p.Outline) < maxOutline {
		p.Outline = append(p.Outline, kind)
	}
	if value.CanInterface() {
		if dot, ok := reflect.TypeAssert[ast.DotExpression](value); ok && len(p.Methods) < maxOutline {
			method := diagnosticChoice(dot.Identifier.Name.String(), "filter", "map", "sort", "reduce",
				"find", "some", "every", "flatMap", "slice", "includes", "join", "split", "$list", "$help")
			if method != diagnosticUnknown {
				p.Methods = append(p.Methods, method)
			}
		}
	}
}
