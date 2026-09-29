package bot

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
	"github.com/complynx/zns-chatbot/platform/internal/passes"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

const passMenuPrefix = "passmenu:"
const passMenuHome = "home"
const passMenuEvents = "events"
const passMenuQueue = "queue"
const passMenuInvitations = "invitations"

type passMenuState struct {
	Notice                i18n.ID                      `json:"notice,omitempty"`
	AdminTargetTelegramID int64                        `json:"admin_target_telegram_id,omitempty"`
	Assignment            *passbooking.AdminAssignment `json:"assignment,omitempty"`
	Event                 string                       `json:"event,omitempty"`
	View                  string                       `json:"view"`
	After                 string                       `json:"after,omitempty"`
	Offset                int                          `json:"offset,omitempty"`
	Previous              []string                     `json:"previous,omitempty"`
	PaymentAdmin          string                       `json:"payment_admin,omitempty"`
}

type passMenuAction struct {
	Export      bool                         `json:"export,omitempty"`
	Assignment  *passbooking.AdminAssignment `json:"assignment,omitempty"`
	PaymentFile *registrationProofReference  `json:"payment_file,omitempty"`
	View        *passMenuState               `json:"view,omitempty"`
	Command     *passbooking.Command         `json:"command,omitempty"`
	Profile     *passes.Command              `json:"profile,omitempty"`
}

func isPassMenuUpdate(in incoming, update telegram.Update) bool {
	return in.text == "/passes" || (update.Callback != nil && strings.HasPrefix(in.text, passMenuPrefix))
}

func (b *Bot) passMenuState(ctx context.Context, owner string) (passMenuState, int64, error) {
	state := passMenuState{View: passMenuEvents}
	var revision int64
	err := b.DB.QueryRow(ctx, `SELECT state,revision FROM bot.pass_views WHERE owner=$1`, owner).Scan(&state, &revision)
	if errors.Is(err, pgx.ErrNoRows) {
		err = nil
	}
	return state, revision, err
}

func (b *Bot) storePassMenu(ctx context.Context, owner string, chat, revision int64, state passMenuState) error {
	_, err := b.DB.Exec(ctx, `INSERT INTO bot.pass_views(owner,chat_id,revision,state) VALUES($1,$2,$3,$4)
	ON CONFLICT(owner) DO UPDATE SET chat_id=$2,revision=$3,state=$4 WHERE bot.pass_views.revision<=$3`, owner, chat, revision, state)
	return err
}

func (b *Bot) handlePassMenu(ctx context.Context, in incoming, update telegram.Update) error {
	state, revision, err := b.passMenuState(ctx, in.owner)
	if err != nil {
		return err
	}
	var notice i18n.ID
	if update.Callback == nil {
		state = passMenuState{View: passMenuEvents}
	} else {
		state, notice, err = b.applyPassMenu(ctx, in, update.ID, state, revision)
		if err != nil {
			return err
		}
	}
	state.Notice = notice
	if err = b.storePassMenu(ctx, in.owner, in.chat, update.ID, state); err != nil {
		return err
	}
	if err = b.RenderPassMenu(ctx, in.owner, in.chat, notice); err != nil {
		return err
	}
	if update.Callback != nil {
		b.acknowledge(ctx, update.Callback.ID)
	}
	return nil
}

func (b *Bot) applyPassMenu(
	ctx context.Context,
	in incoming,
	id int64,
	state passMenuState,
	revision int64,
) (passMenuState, i18n.ID, error) {
	var action passMenuAction
	token := strings.TrimPrefix(in.text, passMenuPrefix)
	err := b.DB.QueryRow(ctx, `SELECT action FROM bot.pass_buttons WHERE owner=$1 AND token=$2 AND revision=$3`, in.owner, token, revision).
		Scan(&action)
	if errors.Is(err, pgx.ErrNoRows) {
		return state, i18n.RegistrationStale, nil
	}
	if err != nil {
		return state, "", err
	}
	if action.View != nil {
		return *action.View, "", nil
	}
	if action.Export {
		notice, exportErr := b.exportPasses(ctx, in, id)
		return state, notice, exportErr
	}
	if action.PaymentFile != nil {
		if err = b.sendRegistrationProof(ctx, in, *action.PaymentFile); err != nil {
			return state, i18n.RegistrationStale, passMenuFailure(err)
		}
		return state, "", nil
	}
	if action.Profile != nil {
		_, err = b.executeProfileCommand(ctx, in, id, *action.Profile)
		if err == nil {
			err = b.RenderProfile(ctx, in.owner, in.chat)
		}
		return state, "", err
	}
	if action.Assignment != nil {
		command := *action.Assignment
		command.Key = "passmenu-assignment-" + token
		_, err = b.API.AssignPass(ctx, in.owner, command)
		if err != nil {
			return state, i18n.RegistrationStale, passMenuFailure(err)
		}
		state.Assignment = nil
		return state, i18n.RegistrationSaved, nil
	}
	if action.Command == nil {
		return state, i18n.RegistrationStale, nil
	}
	command := *action.Command
	command.Key = "passmenu-" + token
	_, err = b.API.ExecutePassBooking(ctx, in.owner, command)
	if err != nil {
		if registrationProfileFailure(err) {
			state.View = passMenuHome
			return state, i18n.RegistrationProfileRequired, nil
		}
		return state, i18n.RegistrationStale, passMenuFailure(err)
	}
	return state, i18n.RegistrationSaved, nil
}

func passMenuFailure(err error) error {
	if problem, ok := errors.AsType[*core.ProblemError](err); ok && problem.Status < http.StatusInternalServerError {
		return nil
	}
	return err
}

const passStateCancelled = "cancelled"
const passAccept = "accept"
const passPriceParameter = "price"

const passInvite = "invite"
const passDecline = "decline"
