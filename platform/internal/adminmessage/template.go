package adminmessage

import (
	"bytes"
	htmltemplate "html/template"
	texttemplate "text/template"
	"text/template/parse"
)

var errTemplateBudget = rejected("admin_message_template_budget")

// broadcastTemplate uses Go's maintained parser/evaluator. Its bounded data-only
// namespace contains no host methods, filesystem loader, or executable objects.
type broadcastTemplate struct {
	plain  *texttemplate.Template
	html   *htmltemplate.Template
	ranges [][]string
}

func parseBroadcastTemplate(content Content) (broadcastTemplate, error) {
	var result broadcastTemplate

	functions := texttemplate.FuncMap{"index": strictTemplateIndex}
	plain, err := texttemplate.New("broadcast").Funcs(functions).Option("missingkey=error").Parse(content.Text)
	if err != nil {
		return result, rejected("admin_message_template_invalid")
	}
	if len(plain.Templates()) != 1 {
		return result, errTemplateBudget
	}
	budget := 100000
	if err = checkTemplateNode(plain.Tree.Root, 1, 0, &budget, &result.ranges); err != nil {
		return result, err
	}
	if content.ParseMode == parseHTML {
		result.html, err = htmltemplate.New("broadcast").
			Funcs(functions).
			Option("missingkey=error").
			Parse(content.Text)
	} else {
		result.plain = plain
	}
	if err != nil {
		return result, rejected("admin_message_template_invalid")
	}
	const trustedHTML = `{{define "user_link"}}{{if .user_link_username}}<a href="https://t.me/{{.user_link_username}}">{{.user_name}}</a>{{else}}<a href="tg://user?id={{.user_id}}">{{.user_name}}</a>{{end}}{{end}}`
	const trustedPlain = `{{define "user_link"}}{{.user_link}}{{end}}`
	if result.html != nil {
		_, err = result.html.Parse(trustedHTML)
	} else {
		_, err = result.plain.Parse(trustedPlain)
	}
	return result, err
}

// Go's built-in index ignores missingkey=error; keep map absence explicit here.
func strictTemplateIndex(value any, keys ...any) (any, error) {
	for _, key := range keys {
		switch collection := value.(type) {
		case map[string]any:
			name, ok := key.(string)
			if !ok {
				return nil, errTemplateBudget
			}
			value, ok = collection[name]
			if !ok {
				return nil, rejected("admin_message_template_missing_key")
			}
		case []any:
			position, ok := key.(int)
			if !ok || position < 0 || position >= len(collection) {
				return nil, errTemplateBudget
			}
			value = collection[position]
		default:
			return nil, errTemplateBudget
		}
	}
	return value, nil
}

func checkTemplateNode(node parse.Node, multiplier, depth int, budget *int, ranges *[][]string) error {
	if node == nil {
		return nil
	}
	*budget -= multiplier
	if *budget < 0 || depth > 16 {
		return errTemplateBudget
	}
	switch value := node.(type) {
	case *parse.IdentifierNode:
		if !templateHelper(value.Ident) {
			return rejected("admin_message_template_helper_unsupported")
		}
	case *parse.WithNode:
		const reboundContext = 2
		multiplier = max(multiplier, reboundContext) // Range fields must retain the original root context.
	case *parse.RangeNode:
		field, err := templateRangeField(value, multiplier)
		if err != nil {
			return err
		}
		*ranges = append(*ranges, field)
		multiplier *= templateCollectionLimit
	case *parse.TemplateNode:
		if !trustedTemplateCall(value) {
			return errTemplateBudget
		}
	}
	for _, child := range templateChildren(node) {
		if err := checkTemplateNode(child, multiplier, depth+1, budget, ranges); err != nil {
			return err
		}
	}
	return nil
}

func templateHelper(name string) bool {
	switch name {
	case "and", "or", "not", "eq", "ne", "lt", "le", "gt", "ge", "len", "index", "slice":
		return true
	}
	return false
}

func templateRangeField(value *parse.RangeNode, multiplier int) ([]string, error) {
	if multiplier != 1 || len(value.Pipe.Cmds) != 1 || len(value.Pipe.Cmds[0].Args) != 1 {
		return nil, errTemplateBudget
	}
	field, ok := value.Pipe.Cmds[0].Args[0].(*parse.FieldNode)
	if !ok {
		return nil, errTemplateBudget
	}
	return field.Ident, nil
}

func trustedTemplateCall(value *parse.TemplateNode) bool {
	if value.Name != linkField || value.Pipe == nil || len(value.Pipe.Cmds) != 1 || len(value.Pipe.Cmds[0].Args) != 1 {
		return false
	}
	_, ok := value.Pipe.Cmds[0].Args[0].(*parse.DotNode)
	return ok
}

func templateChildren(node parse.Node) []parse.Node {
	switch value := node.(type) {
	case *parse.ListNode:
		if value != nil {
			return value.Nodes
		}
	case *parse.ActionNode:
		return []parse.Node{value.Pipe}
	case *parse.PipeNode:
		children := make([]parse.Node, len(value.Cmds))
		for i, command := range value.Cmds {
			children[i] = command
		}
		return children
	case *parse.CommandNode:
		return value.Args
	case *parse.IfNode:
		return []parse.Node{value.Pipe, value.List, value.ElseList}
	case *parse.WithNode:
		return []parse.Node{value.Pipe, value.List, value.ElseList}
	case *parse.RangeNode:
		return []parse.Node{value.Pipe, value.List, value.ElseList}
	}
	return nil
}

type templateWriter struct{ buffer bytes.Buffer }

func (w *templateWriter) Write(p []byte) (int, error) {
	if w.buffer.Len()+len(p) > templateOutputBytes {
		return 0, errTemplateBudget
	}
	return w.buffer.Write(p)
}

func (t broadcastTemplate) render(fields map[string]any, content Content) (Content, error) {
	if err := validateTemplateData(fields, 0); err != nil {
		return Content{}, err
	}
	for _, path := range t.ranges {
		var value any = fields
		for _, key := range path {
			object, ok := value.(map[string]any)
			if !ok {
				return Content{}, errTemplateBudget
			}
			value = object[key]
		}
		switch value.(type) {
		case []any, map[string]any:
		default:
			return Content{}, errTemplateBudget
		}
	}
	var writer templateWriter
	var err error
	if t.html != nil {
		err = t.html.Execute(&writer, fields)
	} else {
		err = t.plain.Execute(&writer, fields)
	}
	if err != nil {
		return Content{}, rejected("admin_message_template_render_failed")
	}
	content.Text = writer.buffer.String()
	if err = validateContent(content); err != nil {
		return Content{}, rejected("admin_message_template_output_invalid")
	}
	return content, nil
}

func validateTemplateData(value any, depth int) error {
	if depth > templateDataDepth {
		return errTemplateBudget
	}
	switch typed := value.(type) {
	case map[string]any:
		if len(typed) > templateCollectionLimit {
			return errTemplateBudget
		}
		for _, item := range typed {
			if err := validateTemplateData(item, depth+1); err != nil {
				return err
			}
		}
	case []any:
		if len(typed) > templateCollectionLimit {
			return errTemplateBudget
		}
		for _, item := range typed {
			if err := validateTemplateData(item, depth+1); err != nil {
				return err
			}
		}
	case string:
		if len(typed) > templateOutputBytes {
			return errTemplateBudget
		}
	}
	return nil
}
