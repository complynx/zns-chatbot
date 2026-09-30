package botdelivery

import (
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/complynx/zns-chatbot/platform/internal/adminmessage"
	"github.com/complynx/zns-chatbot/platform/internal/delivery"
	"github.com/complynx/zns-chatbot/platform/internal/legacyfood"
)

const (
	maxRequestPayloadBytes  = 2 << 20
	maxRequestEnvelopeBytes = 64 << 10
	MaxRequestBytes         = maxRequestPayloadBytes + maxRequestEnvelopeBytes
)

type Service struct {
	DB            *pgxpool.Pool
	Delivery      delivery.Settings
	AdminMessages adminmessage.Service
	Food          legacyfood.Service
}
type EnqueueRequest struct {
	Owner             string
	Chat              int64
	Operation, Effect string
	Reference         Reference
	Phase             string
}
type CardRequest struct {
	Owner             string
	Chat, Target      int64
	Reference         Reference
	Operation, Effect string
	Child             bool
}
type ResultRequest struct {
	Owner        string
	Chat, Update int64
	Effect       string
	Reference    Reference
	Result       StoredResult
	Target       int64
}
type BeginRequest struct {
	Observed     Intent
	Target       int64
	ExportEvents []string
}
type BeginResult struct {
	Intent Intent
	Ready  bool
}

type ReceiptRequest struct{ Observed Intent }
