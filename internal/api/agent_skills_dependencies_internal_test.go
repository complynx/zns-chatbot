package api

import (
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/agentskills"
	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/registrationdetails"
)

func TestSkillsRequirementsMatchExecutableContracts(t *testing.T) {
	t.Parallel()
	server := &Server{options: Options{SkillsEnabled: true, RegistrationDetails: &registrationdetails.Protector{}}}
	output := httptest.NewRecorder()
	server.manifest(output, nil)
	var manifest struct {
		Modules []struct {
			ID      string           `json:"id"`
			Methods []map[string]any `json:"methods"`
		} `json:"modules"`
	}
	require.NoError(t, json.Unmarshal(output.Body.Bytes(), &manifest))
	methods := make(map[string][]map[string]any)
	for _, module := range manifest.Modules {
		methods[module.ID] = module.Methods
	}
	methods["zinusya_administration"] = administrativeMethods()
	methods["zinusya_payments"] = paymentMethods(true)
	methods["zinusya_order_payments"] = orderEvidenceMethods(true)
	methods["zinusya_knowledge"] = knowledgeMethods(db.KnowledgeCapabilities{CanEdit: true, GlobalAdministrator: true})
	methods["zinusya_pairs"] = append(pairParticipantMethods(), pairInvitationMethods()...)
	methods["zinusya_pairs"] = append(methods["zinusya_pairs"], pairPrerequisiteMethods()...)
	methods["zinusya_pairs"] = append(methods["zinusya_pairs"], pairAssignmentProposalMethods()...)
	methods["zinusya_pairs"] = append(methods["zinusya_pairs"], pairAssignmentMethod())
	available := make(map[string]bool)
	var definitions []agentskills.Definition
	for module, cards := range methods {
		agentskills.Annotate(module, cards)
		for _, card := range cards {
			available[module+"."+card["id"].(string)] = true
			if value, ok := card["skills"]; ok {
				raw, err := json.Marshal(value)
				require.NoError(t, err)
				var skills []agentskills.Definition
				require.NoError(t, json.Unmarshal(raw, &skills))
				definitions = append(definitions, skills...)
			}
		}
	}
	require.NotEmpty(t, definitions)
	published := make(map[string]bool)
	for _, definition := range definitions {
		published[definition.Name] = true
		for _, dependency := range definition.Require {
			require.True(t, available[dependency], "%s requires missing contract %s", definition.Name, dependency)
		}
	}
	documents, err := filepath.Glob("../agentskills/*/SKILL.md")
	require.NoError(t, err)
	require.Len(t, published, len(documents))
	for _, document := range documents {
		require.True(t, published[filepath.Base(filepath.Dir(document))], "unpublished skill %s", document)
	}
}
