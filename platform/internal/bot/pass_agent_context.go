package bot

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

const registrationReadsKind = "registration_reads"
const registrationEventLimit = 20
const registrationContextBytes = 20 * 1024

func (b *Bot) addRegistrationContext(ctx context.Context, in incoming, id int64, input *agent.Input) error {
	events, err := b.API.PassEvents(ctx, in.owner)
	if err != nil {
		return err
	}
	page, next, err := registrationEvents(events, "")
	if err != nil {
		return err
	}
	state, _, err := b.passMenuState(ctx, in.owner)
	if err != nil {
		return err
	}
	reads, err := b.authorizedRegistrationReads(ctx, in.owner, id)
	if err != nil {
		return err
	}
	input.Registration = &agent.RegistrationContext{
		AdminTargetTelegramID: state.AdminTargetTelegramID,
		Events:                page,
		MoreEvents:            next != "",
		EventCursor:           next,
		CurrentEvent:          state.Event,
		PendingPartner:        state.View == passInvite,
		TrustedPartnerIDs:     registrationContacts(in.assetMessage),
		Reads:                 reads,
		Remaining:             agent.MaxRegistrationReads - len(reads),
	}
	return boundRegistrationContext(input.Registration)
}

// Recheck the current projection without restoring omitted payloads or read budget.
func (b *Bot) reauthorizeRegistrationContext(
	ctx context.Context,
	owner string,
	value *agent.RegistrationContext,
) error {
	if value == nil {
		return nil
	}
	if err := b.refreshPassCapabilities(ctx, owner, value); err != nil {
		return err
	}
	for index := range value.Reads {
		if err := b.reauthorizeRegistrationRead(ctx, owner, &value.Reads[index]); err != nil {
			return err
		}
	}
	return boundRegistrationContext(value)
}

func (b *Bot) reauthorizeRegistrationRead(ctx context.Context, owner string, read *agent.RegistrationReadResult) error {
	var err error
	switch read.Request.View {
	case agent.RegistrationTakeoverTarget:
		id, parseErr := strconv.ParseInt(read.Request.Target, 10, 64)
		if parseErr != nil {
			return parseErr
		}
		_, err = b.API.PassTakeoverTarget(ctx, owner, read.Request.Event, id)
	case agent.RegistrationAdminTarget:
		id, parseErr := strconv.ParseInt(read.Request.Target, 10, 64)
		if parseErr != nil {
			return parseErr
		}
		_, err = b.API.PassAdminTarget(ctx, owner, read.Request.Event, id)
	case registrationPaymentQueue:
		_, err = b.API.PassPaymentQueue(ctx, owner, read.Request.Event, "")
	case passMenuQueue:
		_, err = b.API.PassQueue(ctx, owner, read.Request.Event, "")
	}
	if err != nil && passMenuFailure(err) == nil {
		*read = agent.RegistrationReadResult{Request: read.Request, Error: mediaForbidden}
		return nil
	}
	return err
}

func registrationContacts(message *telegram.Message) []int64 {
	ids := []int64{}
	if message == nil {
		return ids
	}
	if message.Contact != nil && message.Contact.UserID > 0 {
		ids = append(ids, message.Contact.UserID)
	}
	origin := message.ForwardOrigin
	if origin != nil && origin.Type == "user" && origin.SenderUser != nil && !origin.SenderUser.IsBot &&
		origin.SenderUser.ID > 0 {
		ids = append(ids, origin.SenderUser.ID)
	}
	return ids
}

func registrationEvents(events []passbooking.Event, cursor string) ([]passbooking.Event, string, error) {
	offset := 0
	if cursor != "" {
		value, err := strconv.Atoi(cursor)
		if err != nil || value < 0 {
			return nil, "", errors.New("invalid event cursor")
		}
		offset = min(value, len(events))
	}
	last := min(offset+registrationEventLimit, len(events))
	page := append([]passbooking.Event{}, events[offset:last]...)
	for index := range page {
		titles := map[string]string{}
		for _, language := range []string{"en", "ru"} {
			titles[language] = passMenuLabel(page[index].Titles[language])
		}
		page[index].Titles = titles
		short := map[string]string{}
		for _, language := range []string{"en", "ru"} {
			short[language] = passMenuLabel(page[index].Title(language, true))
		}
		page[index].ShortTitles = short
		page[index].CountryEmoji = passMenuLabel(page[index].CountryEmoji)
	}
	next := ""
	if last < len(events) {
		next = strconv.Itoa(last)
	}
	return page, next, nil
}

// Every load used as model context must recheck current privileges. The stored
// snapshot preserves the read budget; it never preserves permission to view it.
func (b *Bot) authorizedRegistrationReads(
	ctx context.Context,
	owner string,
	id int64,
) ([]agent.RegistrationReadResult, error) {
	reads := []agent.RegistrationReadResult{}
	err := b.DB.QueryRow(ctx, `SELECT content FROM bot.interactions WHERE owner=$1 AND update_id=$2 AND kind=$3`, owner, id, registrationReadsKind).
		Scan(&reads)
	if errors.Is(err, pgx.ErrNoRows) {
		err = nil
	}
	if err != nil {
		return nil, err
	}
	for index := range reads {
		if err = b.reauthorizeRegistrationRead(ctx, owner, &reads[index]); err != nil {
			return nil, err
		}
	}
	return reads, nil
}

func boundRegistrationContext(value *agent.RegistrationContext) error {
	for index := range value.Reads {
		data, err := json.Marshal(value)
		if err == nil && len(data) <= registrationContextBytes {
			return nil
		}
		read := &value.Reads[index]
		*read = agent.RegistrationReadResult{Request: read.Request, Error: "context_budget", Omitted: true}
	}
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if len(data) > registrationContextBytes {
		return errors.New("registration context exceeds budget")
	}
	return nil
}
