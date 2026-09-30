// Package botdelivery owns durable Bot effect admission and receipt projection.
package botdelivery

import (
	"net/http"
	"time"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"

	"github.com/complynx/zns-chatbot/platform/internal/delivery"
	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

var ErrBinding = &core.ProblemError{Status: http.StatusConflict, Code: "bot_delivery_binding"}
var ErrStale = &core.ProblemError{Status: http.StatusConflict, Code: "bot_delivery_stale"}

type Kind string

const (
	CardIntent     Kind = "card"
	ResultIntent   Kind = "result"
	DocumentIntent Kind = "document"
	IdentityIntent Kind = "identity_unavailable"
)

const (
	phaseSend                = "send"
	phaseEdit                = "edit"
	familyPasses             = "passes"
	passCardReceiptKind      = "pass_card"
	familyRefund             = "order_refund"
	familyRefundRedaction    = "order_refund_redaction"
	familyPassRedaction      = "pass_redaction"
	familyPassExport         = "pass_export"
	familyPassProof          = "pass_proof"
	familyMassage            = "massage"
	familyStatic             = "static"
	familyOrderExport        = "order_export"
	familyModernOrderExport  = "modern_order_export"
	familyOrderProof         = "order_proof"
	familyModernOrderProof   = "modern_order_proof"
	familyFoodOrdersExport   = "food_orders_export"
	familyFoodSummaryExport  = "food_summary_export"
	familyFoodProofMeals     = "food_proof_meals"
	familyFoodProofActivity  = "food_proof_activity"
	familyFoodReviewMeals    = "food_review_meals"
	familyFoodReviewActivity = "food_review_activity"
	codePassSourceStale      = "pass_source_stale"
	foodReviewScope          = "review"
)

// Reference contains domain/private-result references, never wire
// text, file bytes, credentials or a serialized callback.
type Reference struct {
	Refund       *orders.RefundDeliveryRead `json:"refund,omitempty"`
	ProofAttempt string                     `json:"proof_attempt,omitempty"`
	Kind         Kind                       `json:"kind"`
	Family       string                     `json:"family,omitempty"`
	CardKey      string                     `json:"card_key,omitempty"`
	Event        string                     `json:"event,omitempty"`
	Object       string                     `json:"object,omitempty"`
	Update       int64                      `json:"update,omitempty"`
	Revision     int64                      `json:"revision,omitempty"`
	Version      int64                      `json:"version,omitempty"`
	Attempt      int64                      `json:"attempt,omitempty"`
	ResultKind   string                     `json:"result_kind,omitempty"`
	Notice       i18n.ID                    `json:"notice,omitempty"`
	Language     string                     `json:"language,omitempty"`
	Generation   *int64                     `json:"generation,omitempty"`
	Source       *readsource.Derivation     `json:"source,omitempty"`
	Continuation Continuation               `json:"continuation"`
	Authorities  []readsource.Authority     `json:"authorities,omitempty"`
}

type Continuation struct {
	Pass     *PassCardReceipt `json:"pass,omitempty"`
	Document *DocumentReceipt `json:"document,omitempty"`
	Retired  bool             `json:"retired,omitempty"`
	Kind     string           `json:"kind,omitempty"`
	Key      string           `json:"key,omitempty"`
	ID       int64            `json:"id,omitempty"`
	Attempt  int64            `json:"attempt,omitempty"`
	Update   int64            `json:"update,omitempty"`
	Revision int64            `json:"revision,omitempty"`
	ViewHash string           `json:"view_hash,omitempty"`
	Tokens   []string         `json:"tokens,omitempty"`
}

// Observation never presents an enqueue result as a message ID.
type Observation struct {
	Reference delivery.Reference
	State     delivery.Kind
	MessageID int64
}

type Intent struct {
	BotID            int64
	Operation        string
	Effect           string
	Owner            string
	Chat             int64
	Reference        Reference
	State            delivery.Kind
	Phase            string
	Target           int64
	Attempt          int64
	NotBefore        time.Time
	MessageID        int64
	ContinuationDone bool
	Receipt          Continuation
}

func (i Intent) QueueReference() delivery.Reference {
	return delivery.Reference{Owner: delivery.Bot, Key: i.Operation, Effect: i.Effect}
}

func (i Intent) Observation() Observation {
	return Observation{Reference: i.QueueReference(), State: i.State, MessageID: i.MessageID}
}

func (r Reference) Valid(owner string) bool {
	if r.Generation != nil && *r.Generation < 0 {
		return false
	}
	if !readsource.Valid(r.Authorities) {
		return false
	}
	if r.Source != nil && !r.Source.Valid() {
		return false
	}
	if r.Kind == IdentityIntent {
		return owner == "" && r.Update >= 0 && r.Source == nil && r.Generation == nil &&
			r.Notice == i18n.IdentityUnavailable
	}
	if owner == "" {
		return false
	}
	switch r.Kind {
	case IdentityIntent:
		return false // Identity references were validated before the owner check.
	case CardIntent:
		return r.Family != "" && r.CardKey != ""
	case ResultIntent:
		return r.ResultKind != "" && r.Generation != nil
	case DocumentIntent:
		if r.Generation == nil {
			return false
		}
		switch r.Family {
		case familyOrderExport,
			familyModernOrderExport,
			familyOrderProof,
			familyModernOrderProof,
			familyPassExport,
			familyPassProof,
			familyFoodOrdersExport,
			familyFoodSummaryExport,
			familyFoodProofMeals,
			familyFoodProofActivity,
			familyFoodReviewMeals,
			familyFoodReviewActivity,
			"admin_file":
			return true
		default:
			return false
		}
	default:
		return false
	}
}

type DocumentReceipt struct {
	Filename string `json:"filename"`
	SHA256   string `json:"sha256"`
	Bytes    int    `json:"bytes"`
}
type StoredResult struct {
	Generation int64                  `json:"generation"`
	Payload    telegram.Send          `json:"payload"`
	Notice     i18n.ID                `json:"notice,omitempty"`
	Values     map[string]string      `json:"values,omitempty"`
	Source     *readsource.Derivation `json:"source,omitempty"`
}

type ModernReceipt struct {
	Status    string `json:"status"`
	EventID   string `json:"event_id"`
	ChatID    int64  `json:"chat_id"`
	Filename  string `json:"filename"`
	SHA256    string `json:"sha256"`
	Bytes     int    `json:"bytes"`
	MessageID int64  `json:"message_id,omitempty"`
}

// PassCardReceipt binds the actual rendered payload's authority and its first
// admitted edit target. It contains no message body or callback data.
type PassCardReceipt struct {
	PreviousMessageID int64  `json:"previous_message_id,omitempty"`
	Event             string `json:"event,omitempty"`
	Capability        string `json:"capability,omitempty"`
}
