package db

import (
	"context"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

// Omitted Latin fields retain old request hashes. An explicit empty pair clears them.
func normalizeProfileLatin(m ProfileMutation) (ProfileMutation, error) {
	if m.FirstNameLatin == nil && m.LastNameLatin == nil {
		return m, nil
	}
	if m.FirstNameLatin == nil || m.LastNameLatin == nil || m.FirstName == "" {
		return m, ErrInvalidProfile
	}
	first, last := strings.TrimSpace(*m.FirstNameLatin), strings.TrimSpace(*m.LastNameLatin)
	if first != "" || last != "" {
		if !validProfileLatinComponent(first) || !validProfileLatinComponent(last) ||
			utf8.RuneCountInString(first+" "+last) > 256 {
			return m, ErrInvalidProfile
		}
	}
	m.FirstNameLatin, m.LastNameLatin = &first, &last
	return m, nil
}

func validProfileLatinComponent(value string) bool {
	if !validProfileNameComponent(value) {
		return false
	}
	letter := false
	for _, r := range value {
		switch {
		case unicode.Is(unicode.Latin, r):
			letter = true
		case unicode.IsMark(r) && unicode.Is(unicode.Inherited, r), r == ' ', r == '-', r == '\'', r == '’':
		default:
			return false
		}
	}
	return letter
}

func applyProfileLatin(p *UserProfile, m ProfileMutation) bool {
	first, last := p.FirstNameLatin, p.LastNameLatin
	if m.FirstNameLatin != nil {
		first, last = *m.FirstNameLatin, *m.LastNameLatin
	} else if m.FirstName != "" && (p.FirstName != m.FirstName || p.LastName != m.LastName) {
		first, last = "", ""
	}
	changed := first != p.FirstNameLatin || last != p.LastNameLatin
	p.FirstNameLatin, p.LastNameLatin = first, last
	return changed
}

// Save after the original-name write: its compatibility trigger clears stale Latin.
// Both statements and the receipt run inside the same owner-locked transaction.
func saveProfileWithLatin(ctx context.Context, tx pgx.Tx, p UserProfile) error {
	if err := saveProfile(ctx, tx, p); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `UPDATE shuttle_user_profiles SET first_name_latin=$2,last_name_latin=$3 WHERE user_id=$1`,
		p.UserID, p.FirstNameLatin, p.LastNameLatin)
	return err
}
