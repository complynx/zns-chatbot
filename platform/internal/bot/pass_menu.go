package bot

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/complynx/zns-chatbot/platform/internal/botdelivery"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	"github.com/complynx/zns-chatbot/platform/internal/interaction"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
	"github.com/complynx/zns-chatbot/platform/internal/passes"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

const passMenuPrefix = "passmenu:"
const passMenuHome = "home"
const passMenuEvents = "events"
const passMenuQueue = "queue"
const passMenuInvitations = "invitations"

type passMenuAction struct {
	Export      bool                          `json:"export,omitempty"`
	Assignment  *passbooking.AdminAssignment  `json:"assignment,omitempty"`
	PaymentFile *registrationProofReference   `json:"payment_file,omitempty"`
	View        *interaction.RegistrationMenu `json:"view,omitempty"`
	Command     *passbooking.Command          `json:"command,omitempty"`
	Profile     *passes.Command               `json:"profile,omitempty"`
}

func isPassMenuUpdate(in incoming, update telegram.Update) bool {
	return in.text == "/passes" || (update.Callback != nil && strings.HasPrefix(in.text, passMenuPrefix))
}

func (b *Bot) passMenuState(ctx context.Context, owner string) (interaction.RegistrationMenu, int64, error) {
	saved, revision, err := b.passMenuRecord(ctx, owner)
	return saved.RegistrationMenu, revision, err
}

func (b *Bot) storePassMenu(
	ctx context.Context,
	owner string,
	chat, revision int64,
	state interaction.RegistrationMenu,
) error {
	saved, _, err := b.passMenuRecord(ctx, owner)
	if err != nil {
		return err
	}
	return b.storePassMenuWithSource(ctx, owner, chat, revision, state, saved.Source)
}

func (b *Bot) handlePassMenu(ctx context.Context, in incoming, update telegram.Update) error {
	saved, revision, err := b.passMenuRecord(ctx, in.owner)
	if err != nil {
		return err
	}
	if update.Callback != nil && saved.Redacted {
		if err = b.RenderPassMenu(ctx, in.owner, in.chat, ""); err != nil {
			return err
		}
		return b.acknowledge(ctx, update.Callback.ID)
	}
	state, notice, err := b.passMenuUpdate(ctx, in, update, saved, revision)
	if update.Callback != nil && stalePassMenuSource(err) && saved.Source != nil {
		if err = b.redactPassMenu(ctx, in.owner, in.chat, revision, saved); err != nil {
			return err
		}
		return b.acknowledge(ctx, update.Callback.ID)
	}
	if err != nil {
		return err
	}
	state.Notice = notice
	if err = b.storePassMenu(ctx, in.owner, in.chat, update.ID, state); err != nil {
		return err
	}
	if err = b.RenderPassMenu(ctx, in.owner, in.chat, notice); err != nil {
		return err
	}
	if update.Callback != nil {
		return b.acknowledge(ctx, update.Callback.ID)
	}
	return nil
}

func (b *Bot) passMenuUpdate(
	ctx context.Context,
	in incoming,
	update telegram.Update,
	saved botdelivery.PassMenu,
	revision int64,
) (interaction.RegistrationMenu, i18n.ID, error) {
	if update.Callback != nil {
		return b.applyPassMenu(ctx, in, update.ID, saved.RegistrationMenu, revision)
	}
	state := interaction.RegistrationMenu{View: passMenuEvents}
	return state, "", b.storePassMenuWithSource(ctx, in.owner, in.chat, update.ID, state, nil)
}

func (b *Bot) applyPassMenu(
	ctx context.Context,
	in incoming,
	id int64,
	state interaction.RegistrationMenu,
	revision int64,
) (interaction.RegistrationMenu, i18n.ID, error) {
	saved, _, err := b.passMenuRecord(ctx, in.owner)
	if err != nil {
		return state, "", err
	}
	if err = b.checkPassMenuDelivery(ctx, in.owner, in.chat, state, saved.Source); err != nil {
		return state, i18n.RegistrationStale, err
	}
	var action passMenuAction
	token := strings.TrimPrefix(in.text, passMenuPrefix)
	err = b.DB.QueryRow(ctx, `SELECT action FROM bot.pass_buttons WHERE owner=$1 AND token=$2 AND revision=$3`, in.owner, token, revision).
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
		_, err = (interaction.RegistrationExecutor{Manual: b.API}).Assignment(ctx, in.owner, command, nil)
		if err != nil {
			return state, i18n.RegistrationStale, passMenuFailure(err)
		}
		return b.storeCommittedPassMenu(ctx, in, id, interaction.SavedPlan{
			RegistrationAssignment: &command, RegistrationMenu: &state,
		}, command.Event, agent.RegistrationAdminAssign)
	}
	if action.Command == nil {
		return state, i18n.RegistrationStale, nil
	}
	command := *action.Command
	command.Key = "passmenu-" + token
	if _, err = b.Host.AdmitPassBooking(ctx, in.owner, command, nil); err != nil {
		return state, i18n.RegistrationStale, passMenuFailure(err)
	}
	_, err = (interaction.RegistrationExecutor{Manual: b.API}).Command(ctx, in.owner, command, nil)
	if err != nil {
		if registrationProfileFailure(err) {
			state.View = passMenuHome
			return state, i18n.RegistrationProfileRequired, nil
		}
		return state, i18n.RegistrationStale, passMenuFailure(err)
	}
	return b.storeCommittedPassMenu(ctx, in, id, interaction.SavedPlan{
		RegistrationCommand: &command, RegistrationMenu: &state,
	}, command.Event, command.Name)
}

// A successful manual receipt can rebuild navigation solely from current domain data.
func (b *Bot) storeCommittedPassMenu(
	ctx context.Context,
	in incoming,
	id int64,
	plan interaction.SavedPlan,
	event, action string,
) (interaction.RegistrationMenu, i18n.ID, error) {
	state, err := b.freshRegistrationOutcomeMenu(ctx, in.owner, plan, event, action)
	if err != nil {
		return state, "", err
	}
	if err = b.storePassMenuWithSource(ctx, in.owner, in.chat, id, state, nil); err != nil {
		return state, "", err
	}
	return state, i18n.RegistrationSaved, nil
}

func passMenuFailure(err error) error {
	// Positive SQL provenance must not become a stale-button notice.
	if core.IsDatabaseFailure(err) {
		return core.ErrDatabase
	}
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
