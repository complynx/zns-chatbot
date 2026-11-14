package db

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
)

const (
	profileSetGender       = "set_gender"
	GenderSourceExplicit   = "explicit"
	GenderSourceInferred   = "inferred"
	profileGenderMaxLength = 128
)

func normalizeGenderMutation(m ProfileMutation) (ProfileMutation, error) {
	if (m.Gender == nil && m.Pronouns == nil) || m.Visible != nil || m.FullName != "" ||
		m.FirstName != "" || m.LastName != "" || m.FirstNameLatin != nil || m.LastNameLatin != nil ||
		m.BotLanguage != "" || m.ExpectationID != uuid.Nil {
		return m, ErrInvalidProfile
	}
	if m.GenderSource != GenderSourceExplicit && m.GenderSource != GenderSourceInferred {
		return m, ErrInvalidProfile
	}
	for _, field := range []**string{&m.Gender, &m.Pronouns} {
		if *field == nil {
			continue
		}
		value := strings.TrimSpace(**field)
		if utf8.RuneCountInString(value) > profileGenderMaxLength || strings.ContainsFunc(value, unicode.IsControl) {
			return m, ErrInvalidProfile
		}
		*field = &value
	}
	return m, nil
}

func applyGenderMutation(p *UserProfile, m ProfileMutation) (bool, error) {
	if m.GenderSource == GenderSourceInferred &&
		(m.Gender != nil && p.GenderSource == GenderSourceExplicit ||
			m.Pronouns != nil && p.PronounsSource == GenderSourceExplicit) {
		return false, ErrConflict
	}
	next := *p
	if m.Gender != nil {
		next.Gender, next.GenderSource = *m.Gender, m.GenderSource
		if next.Gender == "" {
			next.GenderSource = ""
		}
	}
	if m.Pronouns != nil {
		next.Pronouns, next.PronounsSource = *m.Pronouns, m.GenderSource
		if next.Pronouns == "" {
			next.PronounsSource = ""
		}
	} else if m.Gender != nil && p.PronounsSource == GenderSourceInferred &&
		(m.GenderSource == GenderSourceExplicit || p.Gender != next.Gender) {
		// A changed guess or explicit statement does not confirm guessed pronouns.
		next.Pronouns, next.PronounsSource = "", ""
	}
	changed := p.Gender != next.Gender || p.GenderSource != next.GenderSource ||
		p.Pronouns != next.Pronouns || p.PronounsSource != next.PronounsSource
	*p = next
	return changed, nil
}
