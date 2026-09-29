package agent

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"

	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

const RegistrationView = "passes"
const RegistrationRead = "read"
const RegistrationShow = "show"
const RegistrationTakeoverTarget = "takeover_target"
const MaxRegistrationReads = 3

func validateRegistrationPlan(p Plan) error {
	if p.RegistrationAction == nil {
		return nil
	}
	if p.View != RegistrationView || p.Action != nil || p.OrderAction != nil || p.ProfileAction != nil ||
		p.MediaAction != nil ||
		p.KnowledgeAction != nil ||
		p.ScriptAction != nil ||
		p.HistoryAction != nil {
		return errors.New("conflicting registration proposal")
	}
	return validateRegistrationProposal(*p.RegistrationAction)
}

// RegistrationProposal contains references to API evidence, never authorization,
// versions or idempotency keys. The host binds those before execution.
type RegistrationProposal struct {
	Assignment       *RegistrationAssignment `json:"assignment"`
	Name             string                  `json:"name"`
	Event            string                  `json:"event"`
	Target           string                  `json:"target"`
	InviteTelegramID int64                   `json:"invite_telegram_id"`
	PaymentAdmin     string                  `json:"payment_admin"`
	View             string                  `json:"view"`
	Cursor           string                  `json:"cursor"`
}

type RegistrationContext struct {
	Capabilities          []passbooking.Capabilities `json:"capabilities,omitempty"`
	AdminTargetTelegramID int64                      `json:"admin_target_telegram_id,omitempty"`
	Events                []passbooking.Event        `json:"events"`
	MoreEvents            bool                       `json:"more_events"`
	EventCursor           string                     `json:"event_cursor,omitempty"`
	CurrentEvent          string                     `json:"current_event,omitempty"`
	PendingPartner        bool                       `json:"pending_partner"`
	TrustedPartnerIDs     []int64                    `json:"trusted_partner_ids,omitempty"`
	Reads                 []RegistrationReadResult   `json:"reads"`
	Remaining             int                        `json:"remaining"`
}

type RegistrationReadResult struct {
	TakeoverTarget *passbooking.TakeoverTarget `json:"takeover_target,omitempty"`
	AdminTarget    *passbooking.AdminTarget    `json:"admin_target,omitempty"`
	Request        RegistrationProposal        `json:"request"`
	Events         []passbooking.Event         `json:"events,omitempty"`
	Booking        *passbooking.Booking        `json:"booking,omitempty"`
	Invitations    []passbooking.Invitation    `json:"invitations,omitempty"`
	Queue          []passbooking.Booking       `json:"queue,omitempty"`
	PaymentAdmins  []passbooking.Contact       `json:"payment_admins,omitempty"`
	Payment        *passbooking.Payment        `json:"payment,omitempty"`
	PaymentQueue   []passbooking.PaymentReview `json:"payment_queue,omitempty"`
	Next           string                      `json:"next,omitempty"`
	Error          string                      `json:"error,omitempty"`
	Omitted        bool                        `json:"omitted"`
}

func validateRegistrationProposal(p RegistrationProposal) error {
	const maxReference = 100
	for _, value := range []string{p.Event, p.Target, p.PaymentAdmin, p.Cursor} {
		if len(value) > maxReference || strings.ContainsRune(value, 0) {
			return errors.New("invalid registration reference")
		}
	}
	if p.InviteTelegramID < 0 {
		return errors.New("invalid registration contact")
	}
	switch p.View {
	case "",
		"home",
		"events",
		"invitations",
		"queue",
		"admins",
		"profile",
		"payment",
		"payment_queue",
		RegistrationTakeoverTarget,
		RegistrationAdminTarget:
	default:
		return errors.New("invalid registration view")
	}
	if !registrationProposalShape(p) {
		return errors.New("invalid registration action shape")
	}
	if p.Name != RegistrationRead && p.Cursor != "" {
		return errors.New("unexpected registration cursor")
	}
	if p.Event == "" && (p.View != "events" || (p.Name != RegistrationRead && p.Name != RegistrationShow)) {
		return errors.New("registration event required")
	}
	return nil
}

func codexRegistrationFields(data []byte) error {
	fields, err := codexObject(data)
	if err != nil || len(fields) != 8 {
		return errors.New("invalid registration action fields")
	}
	for _, key := range []string{"name", "event", "target", "invite_telegram_id", proposalPaymentAdminField, "view", proposalCursorField} {
		if len(fields[key]) == 0 || bytes.Equal(fields[key], []byte("null")) {
			return errors.New("missing registration action field")
		}
	}
	var proposal RegistrationProposal
	if len(fields["assignment"]) == 0 {
		return errors.New("missing assignment field")
	}
	if !bytes.Equal(fields["assignment"], []byte("null")) {
		if err = validateAssignmentFields(fields["assignment"]); err != nil {
			return err
		}
	}
	if json.Unmarshal(data, &proposal) != nil {
		return errors.New("invalid registration action types")
	}
	return validateRegistrationProposal(proposal)
}

func registrationProposalShape(p RegistrationProposal) bool {
	if p.View == RegistrationTakeoverTarget || p.Name == passbooking.CommandTakeover ||
		p.Name == passbooking.CommandReceivedOnly {
		return p.Assignment == nil && p.PaymentAdmin == "" && p.InviteTelegramID == 0 && p.Target != "" &&
			(p.Name == RegistrationRead || p.Name == RegistrationShow || p.Name == passbooking.CommandTakeover || p.Name == passbooking.CommandReceivedOnly)
	}
	if p.Name == RegistrationAdminAssign || p.Assignment != nil ||
		(p.View == RegistrationAdminTarget && p.Target != "") {
		return registrationAdminShape(p)
	}
	switch p.Name {
	case RegistrationRead, RegistrationShow:
		return p.Target == "" && p.InviteTelegramID == 0 && p.PaymentAdmin == ""
	case "solo", "cancel", "recalculate":
		return p.Target == "" && p.InviteTelegramID == 0
	case "invite":
		return p.InviteTelegramID > 0 && p.Target == ""
	case "accept", "decline", "admin_cancel", "admin_uncouple", "proof_accept", "proof_reject":
		return p.Target != "" && p.InviteTelegramID == 0
	case proposalPaymentAdminField:
		return p.PaymentAdmin != "" && p.Target == "" && p.InviteTelegramID == 0
	default:
		return false
	}
}
