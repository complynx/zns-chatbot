// Package agentskills publishes procedural guidance through authorized tool descriptors.
package agentskills

import (
	"embed"
	"fmt"
	"strings"

	"go.yaml.in/yaml/v3"
)

//go:embed */SKILL.md
var documents embed.FS

// Definition matches Loom's tool-scoped skill contract; source is assigned by Loom.
type Definition struct {
	Name    string   `json:"name" yaml:"name"`
	Usecase string   `json:"usecase" yaml:"usecase"`
	Require []string `json:"require,omitempty" yaml:"require"`
	Body    string   `json:"body" yaml:"-"`
}

type binding struct {
	module, method, name string
}

const (
	methodGet       = "get"
	methodList      = "list"
	knowledgeModule = "zinusya_knowledge"
	profileModule   = "zinusya_profile"
	contextModule   = "zinusya_context"
)

func bindings() []binding {
	return []binding{
		{profileModule, methodGet, "profile-name"},
		{contextModule, methodList, "event-facts"},
		{"zinusya_registrations", "me", "pass-registration"},
		{"zinusya_pairs", "my", "pairing"},
		{"zinusya_payments", "expectations", "pass-payment"},
		{"zinusya_payments", "review_workspace", "pass-payment-review"},
		{"zinusya_order_payments", methodList, "order-payment"},
		{"zinusya_order_payments", "review_workspace", "order-payment-review"},
		{"zinusya_orders", "catalog", "service-order"},
		{"zinusya_meals", "catalog", "meal-order"},
		{"zinusya_massage", "schedule", "massage-booking"},
		{"zinusya_massage_staff", "events", "staff-schedule"},
		{"zinusya_avatars", "drafts", "avatar"},
		{knowledgeModule, "suggest", "knowledge-maintenance"},
		{knowledgeModule, "review", "knowledge-review"},
	}
}

// read parses an immutable embedded document only when its publisher is present.
func read(name string) Definition {
	raw, err := documents.ReadFile(name + "/SKILL.md")
	if err != nil {
		panic(err)
	}
	const frontMatterParts = 3
	parts := strings.SplitN(strings.ReplaceAll(string(raw), "\r\n", "\n"), "---\n", frontMatterParts)
	if len(parts) != frontMatterParts || parts[0] != "" {
		panic("invalid embedded skill front matter: " + name)
	}
	var definition Definition
	decoder := yaml.NewDecoder(strings.NewReader(parts[1]))
	decoder.KnownFields(true)
	if err = decoder.Decode(&definition); err != nil {
		panic(fmt.Errorf("invalid embedded skill %s: %w", name, err))
	}
	definition.Body = strings.TrimSpace(parts[2])
	if definition.Name != name || definition.Usecase == "" || definition.Body == "" {
		panic("incomplete embedded skill: " + name)
	}
	return definition
}

// Visibility keeps account, profile, context, knowledge and file entry points in the overview.
func Visibility(module string) string {
	switch module {
	case "zinusya_account", profileModule, contextModule, knowledgeModule, "zinusya_files":
		return "default"
	default:
		return "list"
	}
}

// Annotate modifies fresh descriptors only, after the caller's authorization filter.
// Requirements never grant authority; Loom rechecks them before advertising or reading skills.
func Annotate(module string, methods []map[string]any) {
	for _, method := range methods {
		id, _ := method["id"].(string)
		method["visibility"] = methodList
		if module == "zinusya_account" || module == "zinusya_files" && id == "info" ||
			module == contextModule && id == methodList || module == profileModule && id == methodGet ||
			module == knowledgeModule && id == "search" {
			method["visibility"] = "default"
		}
		if module == profileModule && id != methodGet && id != "set_language" && id != "operation" {
			method["visibility"] = "hidden"
		}
		for _, item := range bindings() {
			if item.module == module && item.method == id {
				method["skills"] = []Definition{read(item.name)}
			}
		}
	}
}
