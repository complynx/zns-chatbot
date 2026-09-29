package agent

import (
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/complynx/zns-chatbot/platform/internal/passes"
)

const (
	ProfilesView        = "profile"
	maxProfileNameRunes = 200
)

// ProfileContext contains readiness flags, never stored profile or passport values.
type ProfileContext struct {
	History      []passes.Change `json:"history,omitempty"`
	Version      int64           `json:"version"`
	Pending      string          `json:"pending"`
	Frozen       bool            `json:"frozen"`
	HasLegalName bool            `json:"has_legal_name"`
	HasPassport  bool            `json:"has_passport"`
	Role         string          `json:"role"`
}

// ProfileProposal changes only the authenticated user's own editable profile.
// The executor supplies identity and the authoritative version to the Core API.
type ProfileProposal struct {
	Name  string `json:"name"`
	Field string `json:"field"`
	Value string `json:"value"`
}

func validateProfileProposal(plan Plan) error {
	proposal := plan.ProfileAction
	if proposal == nil {
		return nil
	}
	if plan.View != ProfilesView || plan.Action != nil || plan.OrderAction != nil {
		return errors.New("conflicting profile proposal")
	}
	if proposal.Name != "set" || strings.TrimSpace(proposal.Value) == "" ||
		!utf8.ValidString(proposal.Value) || utf8.RuneCountInString(proposal.Value) > maxProfileNameRunes ||
		strings.ContainsFunc(proposal.Value, unicode.IsControl) {
		return errors.New("invalid profile proposal")
	}
	switch proposal.Field {
	case profileLegalNameField, "passport":
	case "role":
		if proposal.Value != "leader" && proposal.Value != "follower" {
			return errors.New("invalid profile role")
		}
	default:
		return errors.New("invalid profile field")
	}
	return nil
}

// This deterministic fixture accepts a deliberately narrow self-introduction.
// Semantic interpretation belongs to the real model; pending forms are hints only.
func scriptedProfilePlan(in Input) (Plan, bool) {
	text := strings.TrimSpace(in.Text)
	lower := strings.ToLower(text)
	for _, prefix := range []string{"my name is ", "меня зовут "} {
		if !strings.HasPrefix(lower, prefix) {
			continue
		}
		plan := Plan{View: ProfilesView, Text: profileFixtureText(in.Language, "clarify")}
		value := strings.TrimSpace(text[len(prefix):])
		if !fixtureSelfName(value) || in.Profile == nil {
			return plan, true
		}
		if in.Profile.Frozen {
			plan.Text = profileFixtureText(in.Language, "frozen")
			return plan, true
		}
		plan.Text = profileFixtureText(in.Language, "propose")
		plan.ProfileAction = &ProfileProposal{Name: "set", Field: profileLegalNameField, Value: value}
		return plan, true
	}
	if strings.Contains(lower, "name") || strings.Contains(lower, "имя") || strings.Contains(lower, "зовут") {
		return Plan{View: ProfilesView, Text: profileFixtureText(in.Language, "clarify")}, true
	}
	return Plan{}, false
}

func fixtureSelfName(value string) bool {
	words := strings.Fields(value)
	if len(words) < 2 || len(words) > 4 || utf8.RuneCountInString(value) > maxProfileNameRunes {
		return false
	}
	for _, word := range words {
		switch strings.ToLower(word) {
		case "and", "or", "и", "или":
			return false
		}
		first, _ := utf8.DecodeRuneInString(word)
		if !unicode.IsUpper(first) {
			return false
		}
		if strings.ContainsFunc(word, func(r rune) bool {
			return !unicode.IsLetter(r) && r != '-' && r != '\''
		}) {
			return false
		}
	}
	return true
}

func profileFixtureText(language, kind string) string {
	english := !strings.HasPrefix(language, "ru")
	switch kind {
	case "propose":
		if english {
			return "I propose saving the name to your profile. It has not been saved yet."
		}
		return "Предлагаю сохранить имя в вашем профиле. Оно пока не сохранено."
	case "frozen":
		if english {
			return "Your profile is locked. I cannot propose a name change."
		}
		return "Профиль заблокирован. Не могу предложить изменение имени."
	default:
		if english {
			return "Please clarify whether this is your own legal name to save. No profile change was proposed."
		}
		return "Уточните, это ваше полное имя для сохранения? Изменение профиля не предложено."
	}
}
