package passbooking

import (
	"strings"
	"unicode/utf8"

	"github.com/complynx/zns-chatbot/platform/internal/passallocation"
)

// AdminAssignment is authorized against global booking administrators. Prices are
// total amounts, including both members when assigning a reciprocal waitlist pair.
type AdminAssignment struct {
	Event         string       `json:"event"`
	Key           string       `json:"key"`
	Version       int64        `json:"version"`
	Target        string       `json:"target"`
	TargetVersion int64        `json:"target_version"`
	TotalPrice    *int         `json:"total_price,omitempty"`
	Kind          *string      `json:"kind,omitempty"`
	Comment       *string      `json:"comment,omitempty"`
	SkipBalance   *bool        `json:"skip_balance,omitempty"`
	AppendTier    *int         `json:"append_tier,omitempty"`
	Create        *AdminCreate `json:"create,omitempty"`
}

// AdminCreate explicitly authorizes creating a known user's missing application.
// ProfileVersion protects profile-derived defaults and explicit name changes.
type AdminCreate struct {
	FromProfile    bool                `json:"from_profile"`
	Role           passallocation.Role `json:"role,omitempty"`
	LegalName      *string             `json:"legal_name,omitempty"`
	ProfileVersion int64               `json:"profile_version"`
}

type AdminAssignmentResult struct {
	Bookings      []Booking `json:"bookings"`
	AssignedCount int       `json:"assigned_count"`
}

const commandAdminAssign = "admin_assign"

func validateAdminAssignment(c AdminAssignment) error {
	const maxKey = 200
	const maxPrice = 1000000000
	const maxKind = 80
	const maxComment = 2000
	if !boundedText(c.Event, maxKey, false) || !boundedText(c.Key, maxKey, false) ||
		!boundedText(c.Target, maxKey, false) || c.Version < 0 || c.TargetVersion < 0 {
		return invalid()
	}
	if c.TotalPrice != nil && (*c.TotalPrice < 0 || *c.TotalPrice > maxPrice) {
		return invalid()
	}
	if c.Kind != nil && !boundedText(*c.Kind, maxKind, false) {
		return invalid()
	}
	if c.Comment != nil && !boundedText(*c.Comment, maxComment, true) {
		return invalid()
	}
	if c.AppendTier != nil && (*c.AppendTier < 1 || c.SkipBalance != nil) {
		return invalid()
	}
	if c.Create != nil {
		return validateAdminCreate(*c.Create)
	}
	return nil
}

func boundedText(value string, limit int, empty bool) bool {
	return utf8.ValidString(value) && !strings.ContainsRune(value, 0) && utf8.RuneCountInString(value) <= limit &&
		(empty || strings.TrimSpace(value) != "")
}

func validateAdminCreate(c AdminCreate) error {
	const maxName = 300
	if c.ProfileVersion < 0 {
		return invalid()
	}
	if c.FromProfile {
		if c.Role != "" || c.LegalName != nil {
			return invalid()
		}
		return nil
	}
	if (c.Role != passallocation.Leader && c.Role != passallocation.Follower) ||
		c.LegalName == nil || !boundedText(*c.LegalName, maxName, false) {
		return invalid()
	}
	return nil
}
