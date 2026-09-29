package legacyfood

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/delivery"
)

const Meals = "meals"
const Activity = "activities"
const Pending = "pending"
const Submitted = "proof_submitted"
const Paid = "paid"
const Rejected = "rejected"

type Service struct {
	Delivery delivery.Settings
	DB       *pgxpool.Pool
	BotID    int64
}

type Event struct {
	ID             string          `json:"id"`
	Menu           json.RawMessage `json:"menu"`
	MenuSHA256     string          `json:"menu_sha256"`
	MealPrices     MealPrices      `json:"meal_prices"`
	ActivityPrices ActivityPrices  `json:"activity_prices"`
	Deadline       time.Time       `json:"deadline"`
	CacaoCapacity  int             `json:"cacao_capacity"`
	Active         bool            `json:"active"`
}

type Payment struct {
	Kind            string     `json:"kind"`
	Generation      int64      `json:"generation"`
	Status          string     `json:"status"`
	ProofID         string     `json:"proof_id,omitempty"`
	ProofSource     string     `json:"proof_source,omitempty"`
	Receiver        string     `json:"receiver,omitempty"`
	ReceivedAt      *time.Time `json:"received_at,omitempty"`
	ConfirmedBy     string     `json:"confirmed_by,omitempty"`
	ConfirmedAt     *time.Time `json:"confirmed_at,omitempty"`
	RejectedBy      string     `json:"rejected_by,omitempty"`
	RejectedAt      *time.Time `json:"rejected_at,omitempty"`
	LegacySourceKey string     `json:"legacy_source_key,omitempty"`
}

func (p Payment) Locked() bool { return p.Status == Submitted || p.Status == Paid }

type Order struct {
	ID              string        `json:"id"`
	EventID         string        `json:"event_id"`
	Owner           string        `json:"owner"`
	Version         int64         `json:"version"`
	Meals           MealSelection `json:"meals"`
	MealTotal       Amount        `json:"meal_total"`
	Complete        bool          `json:"complete"`
	Activities      Activities    `json:"activities"`
	ActivityTotal   Amount        `json:"activity_total"`
	PaymentAdmin    string        `json:"payment_admin,omitempty"`
	CreatedAt       time.Time     `json:"created_at"`
	LastUpdated     *time.Time    `json:"last_updated,omitempty"`
	MealPayment     Payment       `json:"meal_payment"`
	ActivityPayment Payment       `json:"activity_payment"`
}

type Command struct {
	CatalogRevision string        `json:"catalog_revision,omitempty"`
	EventID         string        `json:"event_id"`
	OrderID         string        `json:"order_id,omitempty"`
	Name            string        `json:"name"`
	Version         int64         `json:"version"`
	Key             string        `json:"key"`
	Kind            string        `json:"kind,omitempty"`
	Generation      int64         `json:"generation"`
	Meals           MealSelection `json:"meals,omitempty"`
	Activity        string        `json:"activity,omitempty"`
	ProofID         string        `json:"proof_id,omitempty"`
}

func problem(code string) error { return &core.ProblemError{Status: http.StatusConflict, Code: code} }
func forbidden() error          { return &core.ProblemError{Status: http.StatusForbidden, Code: "forbidden"} }
