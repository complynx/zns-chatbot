package knowledge

import (
	"strings"
	"unicode/utf8"
)

func validText(value string, limit int, empty bool) bool {
	return utf8.ValidString(value) && utf8.RuneCountInString(value) <= limit &&
		!strings.ContainsRune(value, 0) && (empty || strings.TrimSpace(value) != "")
}

func validID(value string, empty bool) bool {
	const maxID = 100
	if value == "" {
		return empty
	}
	if len(value) > maxID {
		return false
	}
	for _, c := range value {
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') && c != '_' && c != '-' && c != '.' {
			return false
		}
	}
	return true
}

func validate(c Command, internal bool) error {
	const maxKey = 200
	if !validText(c.Key, maxKey, false) || !validID(c.Event, true) || c.Version < 0 || c.ProposalID < 0 {
		return invalid()
	}
	if c.Name == assess && !internal {
		return forbidden()
	}
	var valid bool
	switch c.Name {
	case Curate, Suggest, RemoveFact:
		valid = validFactCommand(c)
	case Review, assess:
		valid = c.ProposalID > 0 && c.Topic == "" && c.FactKey == "" && validText(c.Text, MaxMemoText, true) &&
			(c.Decision == approve || c.Decision == "reject")
	case DocumentSet, DocumentDelete:
		valid = validDocumentCommand(c)
	case MemoSet, MemoDelete:
		valid = validMemoCommand(c)
	default:
		return invalid()
	}
	if !valid {
		return invalid()
	}
	return nil
}

func validFactCommand(c Command) bool {
	if !validID(c.Topic, false) || !validID(c.FactKey, false) || c.ProposalID != 0 || c.Decision != "" {
		return false
	}
	if c.Name == RemoveFact {
		return c.Text == ""
	}
	return validText(c.Text, MaxText, false) && (c.Name != Suggest || c.Version == 0)
}

func validMemoCommand(c Command) bool {
	if c.Event != "" || c.Topic != "" || !validID(c.FactKey, false) || c.ProposalID != 0 || c.Decision != "" {
		return false
	}
	if c.Name == MemoDelete {
		return c.Text == ""
	}
	return validText(c.Text, MaxMemoText, false)
}

func validDocumentCommand(c Command) bool {
	if c.Event != "" || !validID(c.Topic, false) || !validID(c.FactKey, false) || c.ProposalID != 0 ||
		c.Decision != "" {
		return false
	}
	if c.Name == DocumentDelete {
		return c.Text == ""
	}
	return validText(c.Text, MaxDocumentText, false)
}
