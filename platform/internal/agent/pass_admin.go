package agent

import (
	"bytes"
	"errors"
	"strconv"
	"strings"
	"unicode/utf8"
)

const RegistrationAdminAssign = "admin_assign"
const RegistrationAdminTarget = "admin_target"
const assignmentCreateField = "create"
const assignmentProfileField = "from_profile"
const profileLegalNameField = "legal_name"

// RegistrationAssignment carries semantic options only. Actor, target, profile
// and booking versions are supplied by the authenticated host after a read.
type RegistrationAssignment struct {
	TotalPrice  *int    `json:"total_price"`
	Kind        *string `json:"kind"`
	Comment     *string `json:"comment"`
	SkipBalance *bool   `json:"skip_balance"`
	AppendTier  *int    `json:"append_tier"`
	Create      bool    `json:"create"`
	FromProfile bool    `json:"from_profile"`
	Role        string  `json:"role"`
	LegalName   *string `json:"legal_name"`
}

func validRegistrationAssignment(p *RegistrationAssignment) bool {
	const maxKind = 80
	const maxName = 300
	const maxComment = 2000
	if p == nil || (p.TotalPrice != nil && (*p.TotalPrice < 0 || *p.TotalPrice > 1000000000)) ||
		(p.AppendTier != nil && (*p.AppendTier < 1 || p.SkipBalance != nil)) {
		return false
	}
	if !registrationOptionText(p.Kind, maxKind, false) || !registrationOptionText(p.Comment, maxComment, true) ||
		!registrationOptionText(p.LegalName, maxName, false) {
		return false
	}
	if !p.Create {
		return !p.FromProfile && p.Role == "" && p.LegalName == nil
	}
	if p.FromProfile {
		return p.Role == "" && p.LegalName == nil
	}
	return (p.Role == "leader" || p.Role == "follower") && p.LegalName != nil
}

func registrationOptionText(value *string, limit int, empty bool) bool {
	return value == nil || (utf8.ValidString(*value) && !strings.ContainsRune(*value, 0) &&
		utf8.RuneCountInString(*value) <= limit && (empty || strings.TrimSpace(*value) != ""))
}

func registrationAdminShape(p RegistrationProposal) bool {
	if p.Name == RegistrationAdminAssign {
		return p.Target != "" && p.InviteTelegramID == 0 && p.PaymentAdmin == "" &&
			validRegistrationAssignment(p.Assignment)
	}
	if p.Assignment != nil {
		return false
	}
	if p.Name == RegistrationShow && p.View == RegistrationAdminTarget {
		return p.Target != "" && p.InviteTelegramID == 0 && p.PaymentAdmin == "" && p.Cursor == ""
	}
	if p.Name == RegistrationRead && p.View == RegistrationAdminTarget {
		id, err := strconv.ParseInt(p.Target, 10, 64)
		return err == nil && id > 0 && p.InviteTelegramID == 0 && p.PaymentAdmin == "" && p.Cursor == ""
	}
	return false
}

func validateAssignmentFields(data []byte) error {
	fields, err := codexObject(data)
	if err != nil || len(fields) != 9 {
		return errors.New("invalid assignment fields")
	}
	for _, name := range []string{"total_price", "kind", "comment", "skip_balance", "append_tier", assignmentCreateField, assignmentProfileField, providerRoleField, profileLegalNameField} {
		if len(fields[name]) == 0 {
			return errors.New("missing assignment field")
		}
	}
	for _, name := range []string{assignmentCreateField, assignmentProfileField, providerRoleField} {
		if bytes.Equal(fields[name], []byte("null")) {
			return errors.New("null assignment field")
		}
	}
	return nil
}
