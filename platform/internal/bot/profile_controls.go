package bot

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"

	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	"github.com/complynx/zns-chatbot/platform/internal/passes"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

const profileControlPrefix = "profile:control:"

func isProfileUpdate(in incoming, update telegram.Update) bool {
	switch in.text {
	case "/profile", "/name", "/legal_name", "/passport", "/role":
		return true
	}
	return update.Callback != nil && strings.HasPrefix(in.text, "profile:")
}

func profileField(text string) string {
	switch text {
	case "/passport":
		return profilePassportField
	case "/role":
		return profileRoleField
	default:
		return profileLegalName
	}
}

func profileHint(field string) i18n.ID {
	switch field {
	case profilePassportField:
		return i18n.RegistrationIdentityDocumentHint
	case profileRoleField:
		return i18n.RegistrationRole
	default:
		return i18n.ProfileNameHint
	}
}

func (b *Bot) profileButtons(
	ctx context.Context,
	owner, language string,
	profile passes.Profile,
) ([][]telegram.Button, error) {
	choices := []struct {
		id           i18n.ID
		field, value string
	}{
		{
			i18n.RegistrationLeader,
			profileRoleField,
			profileLeader,
		},
		{i18n.RegistrationFollower, profileRoleField, profileFollower},
	}
	if !profile.Frozen {
		choices = append([]struct {
			id           i18n.ID
			field, value string
		}{
			{i18n.ProfileSetName, profileLegalName, ""}, {i18n.RegistrationIdentityDocument, profilePassportField, ""},
		}, choices...)
	}
	rows := [][]telegram.Button{}
	tokens := []string{}
	for _, choice := range choices {
		command := passes.Command{
			Name:    profileBegin,
			Field:   choice.field,
			Value:   choice.value,
			Version: profile.Version,
			Origin:  originManual,
		}
		if choice.value != "" {
			command.Name = profileSet
		}
		data, err := json.Marshal(command)
		if err != nil {
			return nil, err
		}
		digest := sha256.Sum256(append([]byte(owner+"/profile/"), data...))
		token := hex.EncodeToString(digest[:16])
		if _, err = b.DB.Exec(
			ctx,
			`INSERT INTO bot.profile_buttons(owner,token,action) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`,
			owner,
			token,
			command,
		); err != nil {
			return nil, err
		}
		label, err := i18n.Translate(language, choice.id, nil)
		if err != nil {
			return nil, err
		}
		rows = append(rows, []telegram.Button{{Text: label, Data: profileControlPrefix + token}})
		tokens = append(tokens, token)
	}
	_, err := b.DB.Exec(ctx, `DELETE FROM bot.profile_buttons WHERE owner=$1 AND NOT(token=ANY($2))`, owner, tokens)
	return rows, err
}

const profilePassportField = "passport"
const profileRoleField = "role"
const profileLeader = "leader"
const profileFollower = "follower"
const profileSet = "set"
