package passbooking

import (
	"slices"
	"strings"
	"time"
)

const MaxReadAuthorities = 256

type ReadAuthorityKind string

const (
	ReadOwnerBooking     ReadAuthorityKind = "owner_booking"
	ReadOwnedEvent       ReadAuthorityKind = "owned_event"
	ReadInvitation       ReadAuthorityKind = "invitation"
	ReadPrivileged       ReadAuthorityKind = "privileged"
	ReadCapability       ReadAuthorityKind = "capability"
	ReadPaymentRole      ReadAuthorityKind = "payment_role"
	ReadOperationTarget  ReadAuthorityKind = "operation_target"
	ReadExportPermission ReadAuthorityKind = "export_permission"
	ReadOwnerMenu        ReadAuthorityKind = "owner_menu"
)

// ReadAuthority identifies the source of derived text without copying its data.
// It is host metadata, never a model-supplied authorization grant.
type ReadAuthority struct {
	Kind             ReadAuthorityKind `json:"kind"`
	Event            string            `json:"event"`
	Owner            string            `json:"owner,omitempty"`
	Version          int64             `json:"version,omitempty"`
	CreatedAt        time.Time         `json:"created_at,omitzero"`
	Action           string            `json:"action,omitempty"`
	TargetTelegramID int64             `json:"target_telegram_id,omitempty"`
	PaymentAttempt   string            `json:"payment_attempt,omitempty"`
}

func ValidReadAuthorities(authorities []ReadAuthority) bool {
	if len(authorities) > MaxReadAuthorities {
		return false
	}
	for _, authority := range authorities {
		if !authority.valid() {
			return false
		}
	}
	return true
}

func (a ReadAuthority) valid() bool {
	if !a.validIdentity() || !a.validPaymentAuthority() {
		return false
	}
	switch a.Kind {
	case ReadOwnerBooking, ReadInvitation:
		return a.Owner != "" && a.Version > 0 && !a.CreatedAt.IsZero() && a.Action == "" && a.TargetTelegramID == 0
	case ReadOwnedEvent, ReadPaymentRole, ReadExportPermission, ReadOwnerMenu:
		return a.Owner == "" && a.Version == 0 && a.CreatedAt.IsZero() && a.Action == "" && a.TargetTelegramID == 0
	case ReadOperationTarget:
		return a.Owner != "" && a.Version == 0 && a.CreatedAt.IsZero() && a.TargetTelegramID > 0 &&
			slices.Contains([]string{commandAdminAssign, commandAdminCancel, commandBatchUncouple,
				commandRecalculate, commandProofAccept, commandProofReject, CommandTakeover,
				CommandReceivedOnly, commandAccept, commandDecline}, a.Action)
	case ReadPrivileged, ReadCapability:
		return a.validPrivilegedIdentity() && a.TargetTelegramID >= 0 &&
			(a.Kind != ReadPrivileged || slices.Contains([]string{commandAdminAssign, commandProofAccept, commandProofReject, CommandTakeover}, a.Action)) &&
			slices.Contains(
				[]string{
					solo,
					commandInvite,
					commandAccept,
					commandDecline,
					capabilityPaymentAdmin,
					capabilityCancel,
					commandAdminCancel,
					commandBatchUncouple,
					commandRecalculate,
					commandProofAccept,
					commandProofReject,
					commandAdminAssign,
					CommandTakeover,
					CommandReceivedOnly,
				},
				a.Action,
			) &&
			(a.TargetTelegramID == 0 || a.Action == commandAdminAssign || a.Action == CommandTakeover)
	default:
		return false
	}
}

func (a ReadAuthority) validIdentity() bool {
	const maxIdentity = 200
	return a.Event != "" && len(a.Event) <= maxIdentity && !strings.ContainsRune(a.Event, 0) &&
		len(a.Owner) <= maxIdentity && !strings.ContainsRune(a.Owner, 0)
}

func (a ReadAuthority) validPaymentAuthority() bool {
	const attemptBytes = 64
	if a.PaymentAttempt == "" {
		return true
	}
	return a.Kind == ReadPrivileged && a.Action == commandProofAccept && a.Owner != "" &&
		len(a.PaymentAttempt) == attemptBytes && strings.Trim(a.PaymentAttempt, "0123456789abcdef") == ""
}

func (a ReadAuthority) validPrivilegedIdentity() bool {
	if a.Owner == "" {
		return a.Version == 0 && a.CreatedAt.IsZero()
	}
	return a.Kind == ReadPrivileged && a.Version > 0 && !a.CreatedAt.IsZero()
}
