package bot

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	"github.com/complynx/zns-chatbot/platform/internal/massage"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

const massagePrefix = "massage:"
const massageHome = "home"
const massageMine = "mine"
const massageLength = "length"
const massageProviders = "providers"
const massageClients = "clients"
const massageInstant = "instant"
const massageLegacyExit = "legacy_exit"

func isMassageUpdate(in incoming, update telegram.Update) bool {
	return in.text == "/massage" ||
		(update.Callback != nil && (strings.HasPrefix(in.text, massagePrefix) || strings.HasPrefix(in.text, "massage|")))
}

type massageView struct {
	LegacyBooking string `json:"legacy_booking,omitempty"`
	LegacyID      string `json:"legacy_id,omitempty"`
	Event         string `json:"event"`
	View          string `json:"view"`
	Party         string `json:"party,omitempty"`
	Length        int    `json:"length,omitempty"`
	Specialist    string `json:"specialist,omitempty"`
	Page          int    `json:"page,omitempty"`
}

type massageButtonAction struct {
	Legacy      *massage.LegacyCommand `json:"legacy,omitempty"`
	View        *massageView           `json:"view,omitempty"`
	Command     *massage.Command       `json:"command,omitempty"`
	Preferences *massage.Preferences   `json:"preferences,omitempty"`
}

func (b *Bot) massageState(ctx context.Context, owner string) (massageView, int64, error) {
	state := massageView{Event: b.currentOrderEvent(), View: massageHome}
	var revision int64
	err := b.DB.QueryRow(ctx, `SELECT state,revision FROM bot.massage_views WHERE owner=$1`, owner).
		Scan(&state, &revision)
	if errors.Is(err, pgx.ErrNoRows) {
		err = nil
	}
	return state, revision, err
}

func (b *Bot) saveMassageState(ctx context.Context, owner string, chat, revision int64, state massageView) error {
	_, err := b.DB.Exec(ctx, `INSERT INTO bot.massage_views(owner,chat_id,revision,state) VALUES($1,$2,$3,$4)
	ON CONFLICT(owner) DO UPDATE SET chat_id=$2,revision=$3,state=$4 WHERE bot.massage_views.revision<=$3`, owner, chat, revision, state)
	return err
}

func (b *Bot) handleMassage(ctx context.Context, in incoming, update telegram.Update) error {
	state, revision, err := b.massageState(ctx, in.owner)
	if err != nil {
		return err
	}
	notice := i18n.ID("")
	switch {
	case update.Callback == nil:
		state = massageView{Event: b.currentOrderEvent(), View: massageHome}
	case strings.HasPrefix(in.text, "massage|"):
		state, notice, err = b.applyLegacyMassageCallback(ctx, in, state, update.ID)
		if err != nil {
			return err
		}
	default:
		state, notice, err = b.applyMassageButton(ctx, in, state, revision)
		if err != nil {
			return err
		}
	}
	if err = b.saveMassageState(ctx, in.owner, in.chat, update.ID, state); err != nil {
		return err
	}
	if err = b.RenderMassage(ctx, in.owner, in.chat, notice); err != nil {
		return err
	}
	if update.Callback != nil {
		b.acknowledge(ctx, update.Callback.ID)
	}
	return nil
}

func (b *Bot) applyMassageButton(
	ctx context.Context,
	in incoming,
	state massageView,
	revision int64,
) (massageView, i18n.ID, error) {
	if in.text == massagePrefix+"open" {
		state.View, state.Page = massageHome, 0
		return state, "", nil
	}
	var action massageButtonAction
	token := strings.TrimPrefix(in.text, massagePrefix)
	err := b.DB.QueryRow(ctx, `SELECT action FROM bot.massage_buttons WHERE owner=$1 AND token=$2 AND revision=$3`, in.owner, token, revision).
		Scan(&action)
	if errors.Is(err, pgx.ErrNoRows) {
		return state, i18n.MassageStale, nil
	}
	if err != nil {
		return state, "", err
	}
	if action.View != nil {
		return *action.View, "", nil
	}
	if action.Legacy != nil {
		command := *action.Legacy
		command.Key = "button-" + token
		return b.applyLegacyMassage(ctx, in.owner, state, command)
	}
	if action.Preferences != nil {
		_, err = b.API.SetMassagePreferences(ctx, in.owner, state.Event, *action.Preferences)
		return massageFailure(state, i18n.MassagePreferences, err)
	}
	if action.Command == nil {
		return state, i18n.MassageStale, nil
	}
	command := *action.Command
	command.Key = "massage-" + token
	_, err = b.API.ExecuteMassage(ctx, in.owner, command)
	if err != nil {
		return massageFailure(state, "", err)
	}
	state.View, state.Page = massageMine, 0
	if command.Action == mediaCancel {
		return state, i18n.MassageCancelled, nil
	}
	return state, i18n.MassageSaved, nil
}

func massageFailure(state massageView, notice i18n.ID, err error) (massageView, i18n.ID, error) {
	if err == nil {
		return state, notice, nil
	}
	if problem, ok := errors.AsType[*core.ProblemError](err); ok && problem.Status < http.StatusInternalServerError {
		return state, i18n.MassageStale, nil
	}
	return state, "", err
}
