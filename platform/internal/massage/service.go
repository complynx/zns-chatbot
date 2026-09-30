package massage

import (
	"context"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/delivery"
)

type Service struct {
	Delivery delivery.Settings
	DB       *pgxpool.Pool
	Now      func() time.Time
}

func (s Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

type EventParty struct {
	ID         string    `json:"id"`
	Event      string    `json:"event"`
	Start      time.Time `json:"start"`
	End        time.Time `json:"end"`
	Tables     int       `json:"tables"`
	Open       bool      `json:"open"`
	DailyLimit int       `json:"daily_limit"`
}

func (p EventParty) rules() Party { return Party{Start: p.Start, End: p.End, Tables: p.Tables} }

type Provider struct {
	Owner           string            `json:"owner"`
	ID              int64             `json:"id"`
	Name            string            `json:"name"`
	Icon            string            `json:"icon"`
	About           map[string]string `json:"about"`
	MinLength       int               `json:"min_length"`
	MaxLength       int               `json:"max_length"`
	LegacyTableFlag bool              `json:"legacy_table_flag"`
	NotifyBookings  bool              `json:"-"`
	NotifyNext      bool              `json:"-"`
	Work            []Span            `json:"work"`
}

type Reservation struct {
	ID          string     `json:"id"`
	Event       string     `json:"event"`
	Party       string     `json:"party"`
	Owner       string     `json:"owner"`
	Specialist  string     `json:"specialist"`
	Slot        int        `json:"slot"`
	Length      int        `json:"length"`
	Start       time.Time  `json:"start"`
	End         time.Time  `json:"end"`
	Price       int        `json:"price"`
	PriceRUB    int        `json:"price_rub"`
	Version     int64      `json:"version"`
	CancelledAt *time.Time `json:"cancelled_at,omitempty"`
	Instant     bool       `json:"instant"`
}

type Command struct {
	// ExpectedStart prevents a changed party schedule from reinterpreting a chosen slot.
	ExpectedStart *time.Time `json:"expected_start,omitempty"`
	Key           string     `json:"key"`
	Action        string     `json:"action"`
	Event         string     `json:"event"`
	Party         string     `json:"party,omitempty"`
	Booking       string     `json:"booking,omitempty"`
	Specialist    string     `json:"specialist,omitempty"`
	Slot          int        `json:"slot,omitempty"`
	Length        int        `json:"length,omitempty"`
	Version       int64      `json:"version,omitempty"`
}

// PublicProvider contains professional contact/display data used for selection.
// Scheduling internals and notification preferences are not public fields.
type PublicProvider struct {
	Owner     string            `json:"owner"`
	ID        int64             `json:"id"`
	Name      string            `json:"name"`
	Icon      string            `json:"icon"`
	About     map[string]string `json:"about"`
	MinLength int               `json:"min_length"`
	MaxLength int               `json:"max_length"`
}

func publicProviders(providers []Provider) []PublicProvider {
	result := make([]PublicProvider, 0, len(providers))
	for _, provider := range providers {
		result = append(result, PublicProvider{
			Owner: provider.Owner, ID: provider.ID, Name: provider.Name, Icon: provider.Icon, About: provider.About,
			MinLength: provider.MinLength, MaxLength: provider.MaxLength,
		})
	}
	return result
}

type Availability struct {
	Party     EventParty       `json:"party"`
	Providers []PublicProvider `json:"providers"`
	Slots     []SlotChoice     `json:"slots"`
}
type SlotChoice struct {
	Slot       int       `json:"slot"`
	Specialist string    `json:"specialist"`
	Start      time.Time `json:"start"`
}

func problem(status int, code string) error { return &core.ProblemError{Status: status, Code: code} }

func authenticated(ctx context.Context, q queryer, actor string) error {
	var exists bool
	if err := q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM core.users WHERE id=$1)`, actor).Scan(&exists); err != nil {
		return core.DatabaseOperationError(err)
	}
	if !exists {
		return problem(http.StatusForbidden, "forbidden")
	}
	return nil
}

type queryer interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}
