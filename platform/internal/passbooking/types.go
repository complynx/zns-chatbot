// Package passbooking owns transactional event registration and queue assignment.
package passbooking

import (
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/delivery"
	"github.com/complynx/zns-chatbot/platform/internal/destination"
	"github.com/complynx/zns-chatbot/platform/internal/passallocation"
)

type Service struct {
	Intake                RegistrationIntakeResolver
	RegistrationRetention time.Duration
	AnnouncementBindings  *destination.Bindings
	DB                    *pgxpool.Pool
	Delivery              delivery.Settings
}

type Booking struct {
	Event            string              `json:"event"`
	Owner            string              `json:"owner"`
	TelegramID       int64               `json:"telegram_id"`
	Version          int64               `json:"version"`
	State            string              `json:"state"`
	Role             passallocation.Role `json:"role"`
	Kind             string              `json:"kind"`
	Partner          string              `json:"partner"`
	InvitationTarget int64               `json:"invitation_target"`
	PaymentAdmin     string              `json:"payment_admin"`
	CreatedAt        time.Time           `json:"created_at"`
	AssignedAt       *time.Time          `json:"assigned_at,omitempty"`
	Price            *int                `json:"price,omitempty"`
	TierIndex        *int                `json:"tier_index,omitempty"`
	SkipBalance      *bool               `json:"skip_balance,omitempty"`
	Comment          string              `json:"comment"`
}

// Command never grants authority. Actor identity comes from the authenticated adapter.
// Target is used only for invitation responses and explicitly administrative actions.
type Command struct {
	Name             string `json:"name"`
	Event            string `json:"event"`
	Version          int64  `json:"version"`
	Key              string `json:"key"`
	Target           string `json:"target,omitempty"`
	TargetVersion    int64  `json:"target_version,omitempty"`
	InviteTelegramID int64  `json:"invite_telegram_id,omitempty"`
	// QueueInvitation retains host-only grounding provenance across admission
	// and replay. Ordinary explicit or trusted-contact invitations omit it.
	QueueInvitation bool   `json:"queue_invitation,omitempty"`
	PaymentAdmin    string `json:"payment_admin,omitempty"`
	ProofID         string `json:"proof_id,omitempty"`
	PaymentAttempt  string `json:"payment_attempt,omitempty"`
}

const (
	pending            = "waiting-for-couple"
	waitlist           = "waitlist"
	assigned           = "assigned"
	paid               = "paid"
	cancelled          = "cancelled"
	solo               = "solo"
	couple             = "couple"
	commandInvite      = "invite"
	commandAccept      = "accept"
	commandDecline     = "decline"
	commandAdminCancel = "admin_cancel"
	commandRecalculate = "recalculate"
	commandProof       = "proof"
	commandProofAccept = "proof_accept"
	commandProofReject = "proof_reject"
)

type event struct {
	id        string
	finishes  time.Time
	passport  bool
	rule      passallocation.Rule
	unlimited bool
	tiers     []passallocation.Tier
	admins    map[string]bool
}

type snapshot struct {
	registrationRanks         map[string]int64
	unfinishedRegistrations   map[string]bool
	announcementBindings      *destination.Bindings
	notificationRegistrations []delivery.Registration
	deliveryBotID             int64
	event                     event
	bookings                  map[string]*Booking
	dirty                     map[string]bool
	now                       time.Time
}

func conflict(code string) error { return &core.ProblemError{Status: http.StatusConflict, Code: code} }
func forbidden() error           { return &core.ProblemError{Status: http.StatusForbidden, Code: "forbidden"} }
func invalid() error {
	return &core.ProblemError{Status: http.StatusBadRequest, Code: "pass_booking_invalid"}
}

func (s *snapshot) touch(b *Booking) {
	if !s.dirty[b.Owner] {
		b.Version++
		s.dirty[b.Owner] = true
	}
}
