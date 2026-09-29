package agent_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/complynx/zns-chatbot/platform/internal/workflow"

	"github.com/stretchr/testify/assert"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/api"
)

func TestOpenAIResponsesContract(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, status, kind, plan string
		httpStatus               int
		ok                       bool
	}{
		{"read", "completed", "output_text", `{"text":"Продолжите кнопкой","view":"workflow","action":null}`, 200, true},
		{"media_receipt", "completed", "output_text", `{"text":"Evidence","view":"media","action":null,"order_action":null,"profile_action":null,"media_action":{"media_id":"image","intent":"receipt","amount":"80.00","currency":"BYN","order_id":"","start_ms":0,"end_ms":0,"frame_count":0}}`, 200, true},
		{"media_paid", "completed", "output_text", `{"text":"Evidence","view":"media","media_action":{"media_id":"image","intent":"accept_payment"}}`, 200, false},
		{"media_forged_field", "completed", "output_text", `{"text":"Evidence","view":"media","media_action":{"media_id":"image","intent":"receipt","paid":true}}`, 200, false},
		{"profile_set", "completed", "output_text", `{"text":"Proposed","view":"profile","action":null,"order_action":null,"profile_action":{"name":"set","field":"legal_name","value":"John Smith"}}`, 200, true},
		{"profile_forbidden", "completed", "output_text", `{"text":"Proposed","view":"profile","action":null,"order_action":null,"profile_action":{"name":"set","field":"api_key","value":"secret"}}`, 200, false},
		{"order_create", "completed", "output_text", `{"text":"Заказ","view":"orders","action":null,"order_action":{"name":"create","order_id":"","extra":""}}`, 200, true},
		{"order_add", "completed", "output_text", `{"text":"Услуга","view":"orders","action":null,"order_action":{"name":"add_extra","order_id":"own-order","extra":"shuttle"}}`, 200, true},
		{"order_pay", "completed", "output_text", `{"text":"Оплачено","view":"orders","action":null,"order_action":{"name":"accept","order_id":"own-order","extra":""}}`, 200, false},
		{"order_wrong_view", "completed", "output_text", `{"text":"Заказ","view":"workflow","action":null,"order_action":{"name":"create","order_id":"","extra":""}}`, 200, false},
		{"order_both", "completed", "output_text", `{"text":"Go","view":"orders","action":{"name":"select","slot_id":"massage-1"},"order_action":{"name":"create","order_id":"","extra":""}}`, 200, false},
		{"select", "completed", "output_text", `{"text":"Предлагаю услугу","view":"workflow","action":{"name":"select","slot_id":"massage-1"}}`, 200, true},
		{"confirm", "completed", "output_text", `{"text":"Done","view":"workflow","action":{"name":"confirm","slot_id":"massage-1"}}`, 200, false},
		{"foreign", "completed", "output_text", `{"text":"Go","view":"workflow","action":{"name":"select","slot_id":"unknown"}}`, 200, false},
		{"principal", "completed", "output_text", `{"text":"Go","view":"workflow","action":null,"principal":"admin"}`, 200, false},
		{"incomplete", "incomplete", "output_text", `{}`, 200, false},
		{"refusal", "completed", "refusal", "", 200, false},
		{"malformed", "completed", "output_text", "broken", 200, false},
		{"trailing", "completed", "output_text", `{"text":"ok","view":"workflow","action":null} {}`, 200, false},
		{"upstream", "", "", "", 429, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(
				skillSelectingHandler(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path != "/responses" || r.Header.Get("Authorization") != "Bearer test-key" {
						t.Error("invalid endpoint or auth")
					}
					var body struct {
						Model string `json:"model"`
						Store bool   `json:"store"`
						Max   int    `json:"max_output_tokens"`
						Input []struct {
							Role    string `json:"role"`
							Content string `json:"content"`
						} `json:"input"`
						Text struct {
							Format struct {
								Type   string          `json:"type"`
								Strict bool            `json:"strict"`
								Schema json.RawMessage `json:"schema"`
							} `json:"format"`
						} `json:"text"`
					}
					{
						e := json.NewDecoder(r.Body).Decode(&body)
						assert.NoError(t, e)
					}

					if body.Model != "gpt-6-luna" || body.Store || body.Max != 1600 ||
						body.Text.Format.Type != "json_schema" ||
						!body.Text.Format.Strict {
						t.Error("invalid Responses contract")
					}
					assert.Contains(t, string(body.Text.Format.Schema), `"profile_action"`)
					assert.Contains(t, string(body.Text.Format.Schema), `"legal_name"`)
					var schema struct {
						Required []string `json:"required"`
					}
					assert.NoError(t, json.Unmarshal(body.Text.Format.Schema, &schema))
					assert.ElementsMatch(
						t,
						[]string{
							"text",
							"view",
							"action",
							"order_action",
							"profile_action",
							"media_action",
							"knowledge_action",
							"script_action",
							"history_action",
							"registration_action",
							"lineup_action",
						},
						schema.Required,
					)
					assert.Contains(t, body.Input[0].Content, "OCR never accepts a payment")
					assert.Contains(t, body.Input[0].Content, "profile.pending is a hint")
					assert.Contains(
						t,
						body.Input[0].Content,
						"Do not invent profile buttons or an extra confirmation step",
					)
					if len(body.Input) != 2 || body.Input[0].Role != "developer" ||
						!strings.Contains(body.Input[1].Content, "manual selection") {
						t.Error("context missing")
					}
					api.JSON(
						w,
						tc.httpStatus,
						map[string]any{
							"status": tc.status,
							"output": []any{
								map[string]any{"type": "reasoning"},
								map[string]any{
									"type":    "message",
									"content": []any{map[string]string{"type": tc.kind, "text": tc.plan}},
								},
							},
						},
					)
				}), "receipts", "profile"),
			)
			defer server.Close()
			_, e := (agent.OpenAI{Key: "test-key", BaseURL: server.URL}).Plan(
				context.Background(),
				agent.Input{
					Business: &agent.BusinessCapabilities{CanBook: true},
					Text:     "finish",
					Workflow: workflow.Workflow{State: "draft"},
					Catalog:  []workflow.Slot{{ID: "massage-1"}},
					History:  []agent.Event{{Kind: "input", Content: json.RawMessage(`"manual selection"`)}},
				},
			)
			if (e == nil) != tc.ok {
				t.Fatalf("want success %v; got %v", tc.ok, e)
			}
		})
	}
	if _, e := (agent.OpenAI{}).Plan(context.Background(), agent.Input{}); e == nil {
		t.Fatal("missing key accepted")
	}
}
