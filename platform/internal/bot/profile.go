package bot

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	"github.com/complynx/zns-chatbot/platform/internal/passes"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

const profileCardKey = "profile"

const profileBegin = "begin"
const profileInvalid = "invalid"
const profileNameValue = "name"
const profileLegalName = "legal_name"
const profileAnswer = "profile_answer"

type profileAnswerRecord struct {
	Version int64  `json:"version"`
	Text    string `json:"text"`
}

func (b *Bot) handleProfile(ctx context.Context, in incoming, update telegram.Update) error {
	notice := ""
	if in.text != "/profile" {
		command, err := b.profileBeginCommand(ctx, in, update)
		if err != nil {
			return err
		}
		notice, err = b.executeProfileCommand(ctx, in, update.ID, command)
		if err != nil {
			return err
		}
	}
	if err := b.record(ctx, in.owner, update.ID, "profile_reply", notice); err != nil {
		return err
	}
	if err := b.RenderProfile(ctx, in.owner, in.chat); err != nil {
		return err
	}
	if update.Callback != nil {
		b.acknowledge(ctx, update.Callback.ID)
	}
	return nil
}

// Cache only the initial version, so a retried /name cannot start a new request.
func (b *Bot) profileBeginCommand(ctx context.Context, in incoming, update telegram.Update) (passes.Command, error) {
	command := passes.Command{Name: profileBegin, Field: profileField(in.text), Origin: originManual}
	if update.Callback != nil {
		token := strings.TrimPrefix(in.text, profileControlPrefix)
		err := b.DB.QueryRow(ctx, `SELECT action FROM bot.profile_buttons WHERE owner=$1 AND token=$2`, in.owner, token).
			Scan(&command)
		if errors.Is(err, pgx.ErrNoRows) {
			command.Name = profileInvalid
			return command, nil
		}
		return command, err
	}
	err := b.DB.QueryRow(ctx, `SELECT (content#>>'{}')::bigint FROM bot.interactions WHERE owner=$1 AND update_id=$2 AND kind='profile_begin'`, in.owner, update.ID).
		Scan(&command.Version)
	if err == nil {
		return command, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return passes.Command{}, err
	}
	profile, err := b.API.PassProfile(ctx, in.owner)
	if err != nil {
		return passes.Command{}, err
	}
	if err = b.record(ctx, in.owner, update.ID, "profile_begin", profile.Version); err != nil {
		return passes.Command{}, err
	}
	err = b.DB.QueryRow(ctx, `SELECT (content#>>'{}')::bigint FROM bot.interactions WHERE owner=$1 AND update_id=$2 AND kind='profile_begin'`, in.owner, update.ID).
		Scan(&command.Version)
	return command, err
}

func (b *Bot) executeProfileCommand(
	ctx context.Context,
	in incoming,
	id int64,
	command passes.Command,
) (string, error) {
	return b.executeProfileWithSource(ctx, in, id, command, nil)
}

func (b *Bot) executeProfileWithSource(ctx context.Context, in incoming, id int64,
	command passes.Command, source *readsource.Derivation) (string, error) {
	preference, err := b.API.Preferences(ctx, in.owner)
	if err != nil {
		return "", err
	}
	command.Key = "tg-profile-" + strconv.FormatInt(id, 10)
	var profile passes.Profile
	var executionErr error
	if source == nil {
		profile, executionErr = b.API.ExecutePassProfile(ctx, in.owner, command)
	} else {
		profile, executionErr = b.Host.ExecuteDerivedPassProfile(ctx, in.owner, command, *source)
	}
	return b.profileOutcome(ctx, in, id, command, profile, executionErr, preference.Language)
}

func (b *Bot) profileOutcome(ctx context.Context, in incoming, id int64, command passes.Command,
	profile passes.Profile, executionErr error, language string) (string, error) {
	metadata := struct {
		Action  string `json:"action"`
		Field   string `json:"field"`
		Version int64  `json:"version"`
		Pending string `json:"pending"`
		Error   string `json:"error,omitempty"`
	}{Action: command.Name, Field: command.Field, Version: profile.Version, Pending: profile.Pending}
	if command.Name != profileBegin && command.Name != "submit" && command.Name != profileSet &&
		command.Name != mediaCancel {
		metadata.Action = profileInvalid
	}
	if command.Field != profileLegalName && command.Field != profilePassportField &&
		command.Field != profileRoleField &&
		command.Field != "" {
		metadata.Field = profileInvalid
	}
	noticeID := i18n.ProfileSaved
	switch {
	case executionErr != nil:
		problem, ok := errors.AsType[*core.ProblemError](executionErr)
		if !ok || problem.Status >= http.StatusInternalServerError {
			return "", executionErr
		}
		metadata.Error = problem.Code
		metadata.Version = command.Version
		noticeID = profileErrorNotice(problem.Code)
	case command.Name == profileBegin:
		noticeID = profileHint(command.Field)
	case command.Name == mediaCancel:
		noticeID = i18n.ProfileCancelled
	}
	if err := b.record(ctx, in.owner, id, "profile_action", metadata); err != nil {
		return "", err
	}
	return i18n.Translate(language, noticeID, nil)
}

func profileErrorNotice(code string) i18n.ID {
	switch code {
	case "pass_profile_frozen":
		return i18n.ProfileFrozen
	case "pass_profile_stale", "idempotency_conflict":
		return i18n.ProfileStale
	case "pass_profile_expired":
		return i18n.ProfileExpired
	case mediaForbidden:
		return i18n.ProfileForbidden
	default:
		return i18n.ProfileInvalid
	}
}

// RenderProfile shows identity only in the owner's dedicated plain-text card.
// Passport values never enter this card or profile interaction records.
func (b *Bot) RenderProfile(ctx context.Context, owner string, chat int64) error {
	preference, err := b.API.Preferences(ctx, owner)
	if err != nil {
		return err
	}
	profile, err := b.API.PassProfile(ctx, owner)
	if err != nil {
		return err
	}
	var content json.RawMessage
	var updateID int64
	var kind string
	var native bool
	err = b.DB.QueryRow(ctx, `SELECT content,kind,native_markdown,update_id FROM bot.interactions WHERE owner=$1 AND kind IN ('profile_reply','profile_answer') ORDER BY id DESC LIMIT 1`, owner).
		Scan(&content, &kind, &native, &updateID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if err == nil {
		visible, visibleErr := b.derivedReplyVisible(ctx, owner, updateID)
		if visibleErr != nil {
			return visibleErr
		}
		if !visible {
			content, native = nil, false
		}
	}
	notice, answer, err := profileNotice(content, kind, profile.Version)
	if err != nil {
		return err
	}
	text, err := profileText(preference.Language, profile, notice)
	if err != nil {
		return err
	}
	if answer != "" {
		text += "\n\n" + answer
	}
	payload := telegram.Send{ChatID: chat, Text: text, Markup: telegram.Markup{Rows: [][]telegram.Button{}}}
	if answer != "" && native {
		payload.Text, payload.NativeMarkdown, payload.LiteralPrefix = answer, true, strings.TrimSuffix(text, answer)
		payload = telegram.FormatSend(payload)
	}
	payload.Markup.Rows, err = b.profileButtons(ctx, owner, preference.Language, profile)
	if err != nil {
		return err
	}
	return b.deliverOrderCard(ctx, owner, profileCardKey, payload)
}

// Model answers describe the snapshot used for planning, not future profile state.
// Legacy unversioned answers are omitted; fixed application notices are translated.
func profileNotice(content json.RawMessage, kind string, version int64) (string, string, error) {
	if len(content) == 0 {
		return "", "", nil
	}
	if kind == profileAnswer {
		var answer profileAnswerRecord
		if json.Unmarshal(content, &answer) == nil && answer.Version == version {
			return "", answer.Text, nil
		}
		return "", "", nil
	}
	var notice string
	err := json.Unmarshal(content, &notice)
	return notice, "", err
}

func profileText(language string, profile passes.Profile, notice string) (string, error) {
	ids := []i18n.ID{i18n.ProfileTitle}
	if profile.LegalName == "" {
		ids = append(ids, i18n.ProfileNameMissing)
	} else {
		ids = append(ids, i18n.ProfileName)
	}
	if profile.Frozen {
		ids = append(ids, i18n.ProfileFrozen)
	} else if profile.Pending != "" {
		if profile.ExpiresAt != nil && time.Now().Before(*profile.ExpiresAt) {
			ids = append(ids, profileHint(profile.Pending))
		} else {
			ids = append(ids, i18n.ProfileExpired)
		}
	}
	switch profile.Role {
	case profileLeader:
		ids = append(ids, i18n.RegistrationLeader)
	case profileFollower:
		ids = append(ids, i18n.RegistrationFollower)
	default:
		ids = append(ids, i18n.RegistrationRole)
	}
	if profile.Passport != "" {
		ids = append(ids, i18n.RegistrationIdentityDocumentSaved)
	}
	lines := []string{}
	for _, id := range ids {
		text, err := i18n.Translate(language, id, map[string]string{profileNameValue: profile.LegalName})
		if err != nil {
			return "", err
		}
		lines = append(lines, text)
	}
	localizedNotice, err := profileReplyNotice(language, notice)
	if err != nil {
		return "", err
	}
	if localizedNotice != "" {
		lines = append(lines, localizedNotice)
	}
	return strings.Join(lines, "\n\n"), nil
}

// Only fixed application notices belong in this card. Re-translate persisted
// notices when the owner changes language; never render model prose as a result.
func profileReplyNotice(language, notice string) (string, error) {
	for _, id := range []i18n.ID{i18n.ProfileSaved, i18n.ProfileCancelled, i18n.ProfileStale, i18n.ProfileExpired, i18n.ProfileInvalid, i18n.ProfileForbidden} {
		for _, locale := range i18n.SupportedLocales() {
			candidate, err := i18n.Translate(string(locale), id, nil)
			if err != nil {
				return "", err
			}
			if candidate == notice {
				return i18n.Translate(language, id, nil)
			}
		}
	}
	return "", nil
}
