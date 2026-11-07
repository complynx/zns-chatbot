package api

import "github.com/complynx/zns-chatbot/internal/agentskills"

// skillModules annotates fresh descriptors only after the deployment opts into Loom skills.
func (s *Server) skillModules(modules []any) []any {
	if !s.options.SkillsEnabled {
		return modules
	}
	for _, value := range modules {
		module, ok := value.(map[string]any)
		if !ok {
			continue
		}
		id, ok := module["id"].(string)
		if !ok {
			continue
		}
		module["visibility"] = agentskills.Visibility(id)
		switch methods := module[manifestMethods].(type) {
		case []map[string]any:
			agentskills.Annotate(id, methods)
		case []any:
			cards := make([]map[string]any, 0, len(methods))
			for _, method := range methods {
				if card, valid := method.(map[string]any); valid {
					cards = append(cards, card)
				}
			}
			agentskills.Annotate(id, cards)
		}
	}
	return modules
}

// Dynamic callers supply only methods authorized by their existing discovery filters.
func (s *Server) skillDiscoveryResult(
	module string,
	methods []map[string]any,
) (*agentDiscoveryResponse, error) {
	if s.options.SkillsEnabled {
		agentskills.Annotate(module, methods)
	}
	return discoveryResult(methods)
}
