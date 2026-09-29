// Package modelsettings manages explicit model permissions and dialogue choices.
package modelsettings

import (
	"context"
	"slices"
)

const (
	Own             = "own"
	Others          = "others"
	Global          = "global"
	GrantPermission = "grant"
)

const DefaultModel = "gpt-6-luna"
const GlobalScope = "*"

type Selection struct {
	Model  string `json:"model"`
	Effort string `json:"effort"`
}

type Option struct {
	Model   string   `json:"model"`
	Efforts []string `json:"efforts"`
}

// Catalog is intentionally bounded to models supported by both deployed adapters.
func Catalog() []Option {
	efforts := []string{"low", "medium", "high", "xhigh", "max"}
	return []Option{
		{"gpt-6-luna", slices.Clone(efforts)},
		{"gpt-6-sol", append(slices.Clone(efforts), "ultra")},
		{"gpt-6-astra", append(slices.Clone(efforts), "ultra")},
	}
}

func Valid(s Selection) bool {
	if s.Model == "" {
		return s.Effort == ""
	}
	for _, option := range Catalog() {
		if option.Model == s.Model {
			if s.Effort == "" {
				return true
			}
			return slices.Contains(option.Efforts, s.Effort)
		}
	}
	return false
}

type selectionKey struct{}

// WithSelection is called only by the trusted host after resolving persistent ACLs.
func WithSelection(ctx context.Context, selection Selection) context.Context {
	if !Valid(selection) || selection.Model == "" {
		selection = Selection{Model: DefaultModel}
	}
	return context.WithValue(ctx, selectionKey{}, selection)
}

func FromContext(ctx context.Context) Selection {
	if selection, ok := ctx.Value(selectionKey{}).(Selection); ok {
		return selection
	}
	return Selection{Model: DefaultModel}
}
