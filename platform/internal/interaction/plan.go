package interaction

import (
	"errors"
	"time"

	"github.com/complynx/zns-chatbot/platform/internal/readsource"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
	"github.com/complynx/zns-chatbot/platform/internal/passes"
)

const CurrentFormatVersion = 1

var ErrUnsupportedFormat = errors.New("unsupported saved turn format")

// ErrInvalidSavedTurn marks a current-format row that can never become valid by
// retrying. Callers must fail closed; replay never falls back to planning again.
var ErrInvalidSavedTurn = errors.New("invalid saved turn")

type SavedPlan struct {
	FormatVersion             int                          `json:"format_version"`
	Kind                      PlanKind                     `json:"-"`
	State                     PlanState                    `json:"-"`
	TerminalReason            TerminalReason               `json:"-"`
	PassAuthority             *PlanAuthority               `json:"pass_authority,omitempty"`
	HistoryGeneration         int64                        `json:"-"`
	MediaResolvedFood         *agent.FoodReceiptTarget     `json:"media_resolved_food,omitempty"`
	SystemNotice              i18n.ID                      `json:"system_notice,omitempty"`
	RegistrationAssignment    *passbooking.AdminAssignment `json:"registration_assignment,omitempty"`
	RegistrationCommand       *passbooking.Command         `json:"registration_command,omitempty"`
	RegistrationMenu          *RegistrationMenu            `json:"registration_menu,omitempty"`
	AVIDs                     []string                     `json:"av_ids,omitempty"`
	MediaID                   string                       `json:"media_id,omitempty"`
	MediaCandidates           []agent.MediaCandidate       `json:"media_candidates,omitempty"`
	MediaSelected             string                       `json:"media_selected,omitempty"`
	MediaResolvedOrder        string                       `json:"media_resolved_order,omitempty"`
	MediaResolvedRegistration string                       `json:"media_resolved_registration,omitempty"`
	MediaResolvedVersion      int64                        `json:"media_resolved_version,omitempty"`
	ProfileVersion            int64                        `json:"profile_version"`
	Plan                      agent.Plan                   `json:"plan"`
	Version                   int64                        `json:"version"`
	OrderCommand              *orders.Command              `json:"order_command,omitempty"`
	ProfileCommand            *passes.Command              `json:"profile_command,omitempty"`
	KnowledgeCommand          *knowledge.Command           `json:"knowledge_command,omitempty"`
}

type PlanKind string
type PlanState string

const (
	DerivedPlan     PlanKind  = "derived"
	CommandPlan     PlanKind  = "command"
	NoticePlan      PlanKind  = "notice"
	TerminalPlan    PlanKind  = "terminal"
	Ready           PlanState = "ready"
	PrivacyTerminal PlanState = "privacy_terminal"
)

// BindKind is called once after host command binding, before durable storage.
// Loading and rendering use the explicit saved kind and never infer provenance.
func (p *SavedPlan) BindKind() {
	p.FormatVersion = CurrentFormatVersion
	// These proposals have already been consumed by host binding/tool execution.
	// Only the bound commands, workflow action and media delivery intent replay.
	p.Plan.OrderAction = nil
	p.Plan.ProfileAction = nil
	p.Plan.RegistrationAction = nil
	p.Plan.KnowledgeAction = nil
	p.Plan.ScriptAction = nil
	p.Plan.HistoryAction = nil
	p.Plan.LineupAction = nil
	p.State = Ready
	switch {
	case p.SystemNotice != "":
		p.Kind = NoticePlan
	case p.commandCount() > 0:
		p.Kind = CommandPlan
	default:
		p.Kind = DerivedPlan
	}
}

func (p *SavedPlan) commandCount() int {
	n := 0
	for _, present := range []bool{p.RegistrationCommand != nil, p.RegistrationAssignment != nil,
		p.KnowledgeCommand != nil, p.ProfileCommand != nil, p.OrderCommand != nil, p.Plan.Action != nil} {
		if present {
			n++
		}
	}
	return n
}

func (p *SavedPlan) Validate() error {
	if p.FormatVersion != CurrentFormatVersion {
		return ErrUnsupportedFormat
	}
	invalid := ErrInvalidSavedTurn
	if c := p.OrderCommand; c != nil && c.Origin == "agent" && (c.Name == "create" || c.Name == "edit") &&
		(c.HistoryGeneration == nil || *c.HistoryGeneration != p.HistoryGeneration) {
		return invalid
	}
	if p.Plan.OrderAction != nil || p.Plan.ProfileAction != nil || p.Plan.RegistrationAction != nil ||
		p.Plan.KnowledgeAction != nil || p.Plan.ScriptAction != nil || p.Plan.HistoryAction != nil || p.Plan.LineupAction != nil {
		return invalid
	}
	if p.HistoryGeneration < 0 {
		return invalid
	}
	if p.State == PrivacyTerminal {
		if p.Kind != TerminalPlan || (p.TerminalReason != HistoryDeleted && p.TerminalReason != SourceRevoked) ||
			p.hasTerminalPayload() {
			return invalid
		}
		return nil
	}
	if p.State != Ready || p.TerminalReason != "" || !p.validKind() {
		return invalid
	}

	if p.Kind != NoticePlan && (p.PassAuthority == nil || p.PassAuthority.Reads == nil ||
		p.PassAuthority.ReadAuthorities == nil || !readsource.Valid(p.PassAuthority.ReadAuthorities)) {
		return invalid
	}
	return nil
}

type PlanAuthority struct {
	PrivateHistory  bool                    `json:"private_history"`
	ReadAuthorities []readsource.Authority  `json:"read_authorities"`
	Reads           []PassContextDependency `json:"reads"`
	Scripts         bool                    `json:"scripts"`
}

type PassContextDependency struct {
	QueueAuthorities []passbooking.ReadAuthority `json:"queue_authorities"`
	TargetBooking    *BookingIdentity            `json:"target_booking,omitempty"`
	Invitations      []InvitationIdentity        `json:"invitations"`
	Booking          *BookingIdentity            `json:"booking,omitempty"`
	Request          agent.RegistrationProposal  `json:"request"`
}

type BookingIdentity struct {
	Event     string    `json:"event"`
	Owner     string    `json:"owner"`
	Version   int64     `json:"version"`
	CreatedAt time.Time `json:"created_at"`
}

type InvitationIdentity struct {
	Owner     string    `json:"owner"`
	Version   int64     `json:"version"`
	CreatedAt time.Time `json:"created_at"`
}

type RegistrationMenu struct {
	Notice                i18n.ID                      `json:"notice,omitempty"`
	AdminTargetTelegramID int64                        `json:"admin_target_telegram_id,omitempty"`
	Assignment            *passbooking.AdminAssignment `json:"assignment,omitempty"`
	Event                 string                       `json:"event,omitempty"`
	View                  string                       `json:"view"`
	After                 string                       `json:"after,omitempty"`
	Offset                int                          `json:"offset,omitempty"`
	Previous              []string                     `json:"previous,omitempty"`
	PaymentAdmin          string                       `json:"payment_admin,omitempty"`
	Historical            bool                         `json:"historical,omitempty"`
}

func (identity BookingIdentity) Matches(booking passbooking.Booking) bool {
	return identity.Version > 0 && identity.Version == booking.Version && identity.Event == booking.Event &&
		identity.Owner == booking.Owner && !identity.CreatedAt.IsZero() && identity.CreatedAt.Equal(booking.CreatedAt)
}

func (p *SavedPlan) validKind() bool {
	switch p.Kind {
	case NoticePlan:
		return p.SystemNotice != "" && p.commandCount() == 0
	case CommandPlan:
		return p.SystemNotice == "" && p.commandCount() == 1
	case DerivedPlan:
		return p.SystemNotice == "" && p.commandCount() == 0
	case TerminalPlan:
		return false
	default:
		return false
	}
}

func (p *SavedPlan) hasTerminalPayload() bool {
	return p.commandCount() != 0 || p.Plan != (agent.Plan{}) || p.SystemNotice != "" || p.PassAuthority != nil ||
		p.MediaResolvedFood != nil || p.RegistrationMenu != nil || len(p.AVIDs) != 0 || p.MediaID != "" ||
		len(
			p.MediaCandidates,
		) != 0 || p.MediaSelected != "" || p.MediaResolvedOrder != "" || p.MediaResolvedRegistration != "" ||
		p.MediaResolvedVersion != 0 || p.ProfileVersion != 0 || p.Version != 0
}
