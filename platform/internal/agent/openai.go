package agent

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/complynx/zns-chatbot/platform/internal/api"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/credits"
	"github.com/complynx/zns-chatbot/platform/internal/modelsettings"
)

const DefaultModel = "gpt-6-luna"

const (
	openAITimeout   = 30 * time.Second
	maxOpenAIBytes  = 1 << 20
	maxOutputTokens = 1600
)

// OpenAI uses Responses structured output. Business tools are executed by the
// bot after validation; this adapter never receives an API user token.
type OpenAI struct {
	Accounting credits.Recorder
	Key        string
	HTTP       *http.Client
	BaseURL    string
}

const planSchema = `{"type":"object","properties":{"lineup_action":{"anyOf":[{"type":"null"},{"type":"object","properties":{"scope":{"type":"string","enum":["current","day","full"]},"date":{"type":"string"},"room":{"type":"string"},"dj":{"type":"string"},"cursor":{"type":"string"}},"required":["scope","date","room","dj","cursor"],"additionalProperties":false}]},"profile_action":{"anyOf":[{"type":"null"},{"type":"object","properties":{"name":{"type":"string","enum":["set"]},"field":{"type":"string","enum":["legal_name","passport","role"]},"value":{"type":"string"}},"required":["name","field","value"],"additionalProperties":false}]},"order_action":{"anyOf":[{"type":"null"},{"type":"object","properties":{"name":{"type":"string","enum":["create","add_extra","remove_extra","export","payment_instructions"]},"order_id":{"type":"string"},"extra":{"type":"string"}},"required":["name","order_id","extra"],"additionalProperties":false}]},"text":{"type":"string"},"view":{"type":"string","enum":["workflow","orders","profile","media","knowledge","passes"]},"action":{"anyOf":[{"type":"null"},{"type":"object","properties":{"name":{"type":"string","enum":["select"]},"slot_id":{"type":"string"}},"required":["name","slot_id"],"additionalProperties":false}]},"media_action":{"anyOf":[{"type":"null"},{"type":"object","properties":{"media_id":{"type":"string"},"intent":{"type":"string","enum":["receipt","avatar","clarify","other","cancel","inspect_video"]},"amount":{"type":"string"},"currency":{"type":"string","enum":["","BYN","RUB"]},"food_kind":{"type":"string","enum":["","meals","activities"]},"registration_event":{"type":"string"},"order_id":{"type":"string"},"start_ms":{"type":"integer"},"end_ms":{"type":"integer"},"frame_count":{"type":"integer"}},"required":["media_id","intent","amount","currency","order_id","registration_event","food_kind","start_ms","end_ms","frame_count"],"additionalProperties":false}]},"knowledge_action":{"anyOf":[{"type":"null"},{"type":"object","properties":{"name":{"type":"string","enum":["read","proposals","memo_read","suggest","curate","remove_fact","memo_set","memo_delete","review_card"]},"event":{"type":"string"},"topic":{"type":"string"},"fact_key":{"type":"string"},"text":{"type":"string"},"proposal_id":{"type":"integer"},"review_queue":{"type":"boolean"},"cursor":{"type":"string"}},"required":["name","event","topic","fact_key","text","proposal_id","review_queue","cursor"],"additionalProperties":false}]},"script_action":{"anyOf":[{"type":"null"},{"additionalProperties":false,"properties":{"code":{"type":"string"},"input_json":{"type":"string"}},"type":"object","required":["code","input_json"]}]},"history_action":{"anyOf":[{"type":"null"},{"additionalProperties":false,"properties":{"before":{"type":"integer"}},"type":"object","required":["before"]}]},"registration_action":{"anyOf":[{"type":"null"},{"additionalProperties":false,"properties":{"assignment":{"anyOf":[{"type":"null"},{"type":"object","properties":{"total_price":{"type":["integer","null"]},"kind":{"type":["string","null"]},"comment":{"type":["string","null"]},"skip_balance":{"type":["boolean","null"]},"append_tier":{"type":["integer","null"]},"create":{"type":"boolean"},"from_profile":{"type":"boolean"},"role":{"type":"string","enum":["","leader","follower"]},"legal_name":{"type":["string","null"]}},"required":["total_price","kind","comment","skip_balance","append_tier","create","from_profile","role","legal_name"],"additionalProperties":false}]},"event":{"type":"string"},"name":{"type":"string","enum":["read","show","solo","invite","accept","decline","payment_admin","cancel","admin_cancel","admin_uncouple","recalculate","proof_accept","proof_reject","admin_assign","takeover","received_only"]},"payment_admin":{"type":"string"},"cursor":{"type":"string"},"view":{"type":"string"},"target":{"type":"string"},"invite_telegram_id":{"type":"integer"}},"type":"object","required":["name","event","target","invite_telegram_id","payment_admin","view","cursor","assignment"]}]}},"required":["lineup_action","text","view","action","order_action","profile_action","media_action","knowledge_action","script_action","history_action","registration_action"],"additionalProperties":false}`

func (m OpenAI) Plan(ctx context.Context, in Input) (Plan, error) {
	if m.Key == "" {
		return Plan{}, errors.New("OPENAI_API_KEY is required")
	}
	ctx, cancel := context.WithTimeout(ctx, openAITimeout)
	defer cancel()
	return planWithSkills(ctx, in, m.structured)
}

const providerRoleField = "role"

func (m OpenAI) structured(ctx context.Context, prompt providerPrompt) (string, error) {
	tokens := maxOutputTokens
	if prompt.name == selectionName {
		tokens = selectionOutputTokens
	}
	selection := modelsettings.FromContext(ctx)
	payload := map[string]any{
		"model":             selection.Model,
		"store":             false,
		"max_output_tokens": tokens,
		"input": []map[string]any{
			{providerRoleField: "developer", "content": prompt.instructions},
			{providerRoleField: "user", "content": openAIContent(prompt.input, prompt.source)},
		},
		codexTextField: map[string]any{
			"format": map[string]any{
				"type":         "json_schema",
				codexNameField: prompt.name,
				"strict":       true,
				"schema":       json.RawMessage(prompt.schema),
			},
		},
	}
	if selection.Effort != "" {
		payload["reasoning"] = map[string]string{"effort": selection.Effort}
	}
	if m.Accounting != nil && m.Accounting.RequestTier() != "" {
		payload["service_tier"] = m.Accounting.RequestTier()
	}
	body, e := json.Marshal(payload)
	if e != nil {
		return "", e
	}
	base := m.BaseURL
	if base == "" {
		base = "https://api.openai.com/v1"
	}
	r, e := http.NewRequestWithContext(ctx, http.MethodPost, base+"/responses", bytes.NewReader(body))
	if e != nil {
		return "", e
	}
	r.Header.Set("Authorization", "Bearer "+m.Key)
	r.Header.Set("Content-Type", "application/json")
	client := m.HTTP
	if client == nil {
		client = &http.Client{Timeout: openAITimeout}
	}
	singleSend := *client
	singleSend.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	call, e := credits.Begin(ctx, m.Accounting, prompt.name, "openai", selection.Model, int64(tokens))
	if e != nil {
		return "", e
	}
	text, responseErr := readMeteredResponse(&singleSend, r, call)
	return text, errors.Join(responseErr, call.Finish(ctx))
}

func readMeteredResponse(singleSend *http.Client, r *http.Request, call *credits.Call) (string, error) {
	resp, e := singleSend.Do(r)
	if e != nil {
		return "", errors.New("OpenAI transport unavailable")
	}
	defer resp.Body.Close()
	call.CaptureRequestID(resp.Header.Get("X-Request-ID"))
	data, readErr := io.ReadAll(io.LimitReader(resp.Body, maxOpenAIBytes+1))
	if readErr != nil || len(data) > maxOpenAIBytes {
		return "", errors.New("invalid OpenAI response")
	}
	call.Capture(credits.ResponsesUsage(data))
	if resp.StatusCode != http.StatusOK {
		return "", errors.New("OpenAI request rejected")
	}
	return readOpenAIText(bytes.NewReader(data))
}

func openAIContent(input []byte, in Input) any {
	images := in.Frames
	if in.Attachment != nil {
		images = []Attachment{*in.Attachment}
	}
	if len(images) == 0 {
		return string(input)
	}
	type part struct {
		Type     string `json:"type"`
		Text     string `json:"text,omitempty"`
		ImageURL string `json:"image_url,omitempty"`
		Detail   string `json:"detail,omitempty"`
	}
	content := []part{{Type: "input_text", Text: string(input)}}
	for _, attachment := range images {
		content = append(content, part{Type: "input_image", ImageURL: "data:" + attachment.MIME + ";base64," +
			base64.StdEncoding.EncodeToString(attachment.Body), Detail: "high"})
	}
	return content
}

type responseEnvelope struct {
	Status string `json:"status"`
	Output []struct {
		Type    string `json:"type"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	} `json:"output"`
}

func readOpenAIText(body io.Reader) (string, error) {
	var response responseEnvelope
	if err := json.NewDecoder(io.LimitReader(body, maxOpenAIBytes)).Decode(&response); err != nil {
		return "", errors.New("invalid OpenAI response")
	}
	if response.Status != "completed" {
		return "", errors.New("OpenAI response incomplete")
	}
	text, err := response.text()
	if err != nil {
		return "", err
	}
	return text, nil
}

func (r responseEnvelope) text() (string, error) {
	var text strings.Builder
	for _, item := range r.Output {
		if item.Type != "message" {
			continue
		}
		for _, content := range item.Content {
			if content.Type == "refusal" {
				return "", errors.New("OpenAI refused the request")
			}
			if content.Type == "output_text" {
				text.WriteString(content.Text)
			}
		}
	}
	return text.String(), nil
}

func decodePlan(text string, catalog []core.Slot) (Plan, error) {
	var plan Plan
	if err := validatePlanFields([]byte(text), ""); err != nil {
		return plan, err
	}
	decoder := json.NewDecoder(strings.NewReader(text))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&plan); err != nil {
		return plan, errors.New("invalid OpenAI plan")
	}
	var tail any
	if !errors.Is(decoder.Decode(&tail), io.EOF) {
		return plan, errors.New("trailing plan data")
	}
	if err := Validate(plan); err != nil {
		return plan, err
	}
	if plan.Action == nil {
		return plan, nil
	}
	for _, slot := range catalog {
		if slot.ID == plan.Action.SlotID {
			return plan, nil
		}
	}
	return plan, errors.New("unknown proposed resource")
}

// ModelHandler is used only on the sandbox container network.
func ModelHandler(model Model) http.Handler {
	mux := http.NewServeMux()
	knowledgeAssessmentRoute(mux, model)
	historySummaryRoute(mux, model)
	broadcastNameRoute(mux, model)
	mux.HandleFunc(
		"GET /healthz",
		func(w http.ResponseWriter, _ *http.Request) { api.JSON(w, http.StatusOK, map[string]bool{"ok": true}) },
	)
	mux.HandleFunc("POST /plan", func(w http.ResponseWriter, r *http.Request) {
		var in Input
		if decodeInput(w, r, &in) != nil {
			api.JSON(w, http.StatusBadRequest, nil)
			return
		}
		ctx, err := modelRequestContext(r)
		if err != nil {
			api.JSON(w, http.StatusBadRequest, nil)
			return
		}
		p, e := model.Plan(ctx, in)
		if e != nil {
			api.JSON(w, http.StatusBadGateway, map[string]string{"error": "model unavailable"})
			return
		}
		api.JSON(w, http.StatusOK, p)
	})
	return mux
}
