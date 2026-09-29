// Package agent exposes typed proposals; a model never supplies identity or confirms writes.
package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/complynx/zns-chatbot/platform/internal/api"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/credits"
	"github.com/complynx/zns-chatbot/platform/internal/legacyfood"
	"github.com/complynx/zns-chatbot/platform/internal/modelsettings"
	"github.com/complynx/zns-chatbot/platform/internal/observability"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
)

type Event struct {
	Kind    string          `json:"kind"`
	Content json.RawMessage `json:"content"`
}

const (
	remoteTimeout   = 40 * time.Second
	maxPlanBytes    = 256 * 1024
	maxInputBytes   = 60000
	maxAVInputBytes = 512000
)

type Input struct {
	HistoryGeneration int64         `json:"-"`
	ModernOrder       *orders.Order `json:"-"`
	ModernOrderCursor string        `json:"-"`
	ModernOrderReview bool          `json:"-"`
	// FoodView is current host-read evidence for script commands, never provider input.
	FoodReviewCursor string                `json:"-"`
	FoodReview       *legacyfood.View      `json:"-"`
	FoodView         *legacyfood.View      `json:"-"`
	Food             *FoodHint             `json:"food_pending_hint,omitempty"`
	Business         *BusinessCapabilities `json:"business,omitempty"`
	// LineupSource is internal transport evidence, removed before provider input.
	LineupSource *LineupContext `json:"lineup_source,omitempty"`
	Lineup       *LineupContext `json:"lineup,omitempty"`
	// Remaining questions in the rolling 24-hour window, after this reservation.
	AssistantQuestionsRemaining *int `json:"assistant_questions_remaining,omitempty"`
	// BeforeProvider is a host-only authorization boundary, never model data.
	BeforeProvider     func(context.Context, *Input) error `json:"-"`
	Registration       *RegistrationContext                `json:"registration,omitempty"`
	Conversation       *HistoryContext                     `json:"conversation,omitempty"`
	Script             *ScriptContext                      `json:"script,omitempty"`
	Knowledge          *KnowledgeContext                   `json:"knowledge,omitempty"`
	Assets             *AssetContext                       `json:"assets,omitempty"`
	AssetTask          *AssetTask                          `json:"asset_task,omitempty"`
	AV                 *AVContext                          `json:"av,omitempty"`
	AVInspection       *AVInspectionContext                `json:"av_inspection,omitempty"`
	MediaContext       *MediaContext                       `json:"media_context,omitempty"`
	Frames             []Attachment                        `json:"frames,omitempty"`
	Attachment         *Attachment                         `json:"attachment,omitempty"`
	Profile            *ProfileContext                     `json:"profile,omitempty"`
	Language           string                              `json:"language,omitempty"`
	OrderCount         int                                 `json:"order_count"`
	EditableOrderCount int                                 `json:"editable_order_count"`
	OrderHistory       []orders.Change                     `json:"order_history,omitempty"`
	Workflow           core.Workflow                       `json:"workflow"`
	Catalog            []core.Slot                         `json:"catalog"`
	History            []Event                             `json:"history"`
	View               string                              `json:"view,omitempty"`
	Orders             []OrderSummary                      `json:"orders,omitempty"`
	Extras             map[string]orders.Extra             `json:"extras,omitempty"`
	OrderContextNotice string                              `json:"order_context_notice,omitempty"`
	// Keep the current utterance after historical context in model JSON.
	Text string `json:"text"`
}
type OrderSummary struct {
	DetailsIncomplete bool          `json:"details_incomplete,omitempty"`
	ID                string        `json:"id"`
	Version           int64         `json:"version"`
	State             string        `json:"state"`
	Choice            orders.Choice `json:"choice"`
}

type OrderProposal struct {
	Name    string `json:"name"`
	OrderID string `json:"order_id"`
	Extra   string `json:"extra"`
}
type Proposal struct {
	Name   string `json:"name"`
	SlotID string `json:"slot_id"`
}
type Plan struct {
	LineupAction       *LineupQuery          `json:"lineup_action,omitempty"`
	RegistrationAction *RegistrationProposal `json:"registration_action,omitempty"`
	HistoryAction      *HistoryProposal      `json:"history_action,omitempty"`
	ScriptAction       *ScriptProposal       `json:"script_action,omitempty"`
	KnowledgeAction    *KnowledgeProposal    `json:"knowledge_action,omitempty"`
	MediaAction        *MediaProposal        `json:"media_action,omitempty"`
	Text               string                `json:"text"`
	View               string                `json:"view"`
	Action             *Proposal             `json:"action,omitempty"`
	OrderAction        *OrderProposal        `json:"order_action,omitempty"`
	ProfileAction      *ProfileProposal      `json:"profile_action,omitempty"`
}
type Model interface {
	Plan(context.Context, Input) (Plan, error)
}

// Remote makes one opaque planning request with a current authorized snapshot.
// The endpoint returns proposals only: it has no host callback or authority to
// execute tools. Further reads or actions must return to the host for a new turn.
type Remote struct {
	Receipts credits.RemoteReceiptVerifier
	Secret   string
	Enforce  bool
	URL      string
	HTTP     *http.Client
}

func (m Remote) Plan(ctx context.Context, in Input) (Plan, error) {
	var p Plan
	if err := refreshRemoteAuthorization(ctx, &in); err != nil {
		return p, err
	}
	b, e := remoteInput(visibleProviderInput(in))
	if e != nil {
		return p, e
	}
	r, e := http.NewRequestWithContext(ctx, http.MethodPost, m.URL+"/plan", bytes.NewReader(b))
	if e != nil {
		return p, errors.New("model endpoint unavailable")
	}
	r.Header.Set("Content-Type", "application/json")
	m.prepareRequest(r)
	selection := modelsettings.FromContext(ctx)
	r.Header.Set(modelHeader, selection.Model)
	r.Header.Set(effortHeader, selection.Effort)
	c := m.HTTP
	if c == nil {
		c = &http.Client{Timeout: remoteTimeout}
	}
	_, diagnostic := observability.StartAgentEvent(ctx,
		observability.AgentEvent{Phase: diagnosticModelPhase, Operation: "model.remote", InputBytes: len(b)})
	defer func() { diagnostic.Finish(e) }()
	resp, e := remoteClient(c).Do(r)
	if e != nil {
		return p, errors.New("model transport unavailable")
	}
	defer resp.Body.Close()
	if e = m.receiveReceipts(ctx, resp); e != nil {
		return p, e
	}
	if resp.StatusCode != http.StatusOK {
		diagnostic.Outcome("error", "unavailable")
		return p, errors.New("model unavailable")
	}
	data, e := io.ReadAll(io.LimitReader(resp.Body, maxPlanBytes+1))
	diagnostic.Result(len(data), 0, false)
	if e != nil || len(data) > maxPlanBytes {
		diagnostic.Outcome("invalid", "invalid_plan")
		return p, errors.New("invalid model response")
	}
	if e = refreshRemoteAuthorization(ctx, &in); e != nil {
		return p, e
	}
	p, e = decodePlan(string(data), in.Catalog)
	if e == nil {
		e = validateVisiblePlan(in, p)
	}
	if e != nil {
		diagnostic.Outcome("invalid", "invalid_plan")
	}
	return p, e
}

func refreshRemoteAuthorization(ctx context.Context, input *Input) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if input.BeforeProvider != nil {
		if err := input.BeforeProvider(ctx, input); err != nil {
			return err
		}
	}
	return ctx.Err()
}
func boundedInput(in Input) ([]byte, error) {
	return boundedInputReserved(in, 0)
}

func boundedInputReserved(in Input, reserve int) ([]byte, error) {
	in.LineupSource = nil
	if err := validateAssetTask(in); err != nil {
		return nil, err
	}
	budget := maxInputBytes
	if in.AV != nil {
		if len(in.AV.Transcript.Text) > 64<<10 {
			return nil, errors.New("AV transcript exceeds budget")
		}
		budget = maxAVInputBytes
	}
	budget -= reserve
	if err := validateFrames(in); err != nil {
		return nil, err
	}
	if len(in.Frames) > 0 {
		frames := make([]Attachment, len(in.Frames))
		copy(frames, in.Frames)
		for i := range frames {
			frames[i].Body = nil
		}
		in.Frames = frames
	}
	if in.Attachment != nil {
		if err := in.Attachment.validate(); err != nil {
			return nil, err
		}
		metadata := *in.Attachment
		metadata.Body = nil
		in.Attachment = &metadata
	}
	b, e := json.Marshal(in)
	if e != nil {
		return nil, e
	}
	// Budget the serialized request, including escaping and current workflow.
	// Drop oldest context, never the user's current message or API state.
	for len(b) > budget && len(in.History)+len(in.OrderHistory) > 0 {
		dropOldestHistory(&in)
		b, e = json.Marshal(in)
		if e != nil {
			return nil, e
		}
	}
	b, e = fitConversationBudget(&in, budget, b)
	if e != nil {
		return nil, e
	}
	if len(b) > budget {
		return nil, errors.New("model input exceeds budget")
	}
	return b, nil
}
func Validate(p Plan) error {
	if err := validateContextPlans(p); err != nil {
		return err
	}
	if len(p.Text) > 3000 ||
		(p.View != workflowView && p.View != OrdersView && p.View != ProfilesView && p.View != MediaView && p.View != KnowledgeView && p.View != RegistrationView) {
		return errors.New("invalid view")
	}
	if p.Action != nil && (p.Action.Name != "select" || len(p.Action.SlotID) > 64) {
		return errors.New("forbidden proposal")
	}
	if p.Action != nil && (p.View != workflowView || p.OrderAction != nil) {
		return errors.New("conflicting proposal")
	}
	if p.KnowledgeAction != nil {
		if p.View != KnowledgeView || p.Action != nil || p.OrderAction != nil || p.ProfileAction != nil ||
			p.MediaAction != nil {
			return errors.New("conflicting knowledge proposal")
		}
		if err := validateKnowledgeProposal(*p.KnowledgeAction); err != nil {
			return err
		}
	}
	if err := validateProfileProposal(p); err != nil {
		return err
	}
	if err := validateMediaProposal(p); err != nil {
		return err
	}
	return validateOrderProposal(p)
}

// ScriptedServer is a deterministic model fixture with deliberately hostile modes.
type ScriptedServer struct {
	mu   sync.Mutex
	mode string
}

func (s *ScriptedServer) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc(
		"GET /healthz",
		func(w http.ResponseWriter, _ *http.Request) { api.JSON(w, http.StatusOK, map[string]bool{"ok": true}) },
	)
	mux.HandleFunc("POST /mode", func(w http.ResponseWriter, r *http.Request) {
		var b struct {
			Mode string `json:"mode"`
		}
		if api.Decode(w, r, &b) != nil {
			api.JSON(w, http.StatusBadRequest, nil)
			return
		}
		if b.Mode != "normal" && b.Mode != "fail" && b.Mode != "forbidden" {
			api.JSON(w, http.StatusBadRequest, nil)
			return
		}
		s.mu.Lock()
		s.mode = b.Mode
		s.mu.Unlock()
		api.JSON(w, http.StatusOK, b)
	})
	mux.HandleFunc("POST /plan", func(w http.ResponseWriter, r *http.Request) {
		var in Input
		if decodeInput(w, r, &in) != nil {
			api.JSON(w, http.StatusBadRequest, nil)
			return
		}
		s.mu.Lock()
		mode := s.mode
		s.mu.Unlock()
		if mode == "fail" {
			api.JSON(w, http.StatusServiceUnavailable, nil)
			return
		}
		p := Plan{View: workflowView}
		if mode == "forbidden" {
			p.Action = &Proposal{Name: "confirm"}
			api.JSON(w, http.StatusOK, p)
			return
		}
		p = scriptedPlan(in)
		api.JSON(w, http.StatusOK, p)
	})
	return mux
}

func scriptedPlan(in Input) Plan {
	if plan, handled := scriptedMediaPlan(in); handled {
		return plan
	}
	if plan, handled := scriptedProfilePlan(in); handled {
		return plan
	}
	if wantsOrders(in) {
		return scriptedOrderPlan(in)
	}
	p := Plan{View: workflowView, Text: workflowHint(in)}
	text := strings.ToLower(in.Text)
	if strings.Contains(text, "отмен") || strings.Contains(text, "cancel") {
		p.Text = "Активной заявки нет: отменять нечего. Можно выбрать услугу кнопкой ниже."
		if in.Workflow.State == "booked" || in.Workflow.State == "draft" {
			p.Text = "Чтобы отменить текущую заявку, нажмите «Отменить заявку» в карточке ниже. Пока она не отменена."
		}
		return p
	}
	if !strings.Contains(text, "выбери") && !strings.Contains(text, "select") {
		return p
	}
	for _, slot := range in.Catalog {
		if matchesSelection(text, slot.ID) {
			p.Action = &Proposal{Name: "select", SlotID: slot.ID}
			p.Text = "Подготовил выбранную услугу. Подтверждение выполняется вашей кнопкой."
			break
		}
	}
	return p
}

func workflowHint(in Input) string {
	switch in.Workflow.State {
	case "draft":
		for _, slot := range in.Catalog {
			if slot.ID == in.Workflow.SlotID && slot.Remaining <= 0 {
				return "Для выбранной услуги сейчас нет свободных мест. Черновик сохранён, но подтвердить его пока нельзя. Выберите другую услугу или дождитесь освобождения места."
			}
		}
		return "Вы уже выбрали услугу вручную или через агента. Черновик сохранён. Проверьте карточку и нажмите «Подтвердить» — я продолжу тот же процесс."
	case "booked":
		return "Бронирование уже завершено. Актуальная карточка ниже; повторная заявка не нужна."
	default:
		return "Выберите услугу кнопкой или напишите «выбери массаж» / «выбери трансфер». Я подготовлю карточку для подтверждения."
	}
}

func matchesSelection(text, slot string) bool {
	return (strings.Contains(text, "массаж") || strings.Contains(text, "massage")) && slot == "massage-1" ||
		(strings.Contains(text, "трансфер") || strings.Contains(text, "shuttle")) && slot == "shuttle-1"
}

const workflowView = "workflow"
