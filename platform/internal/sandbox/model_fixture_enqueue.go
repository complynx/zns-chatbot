package sandbox

import (
	"context"
	"errors"
	"strconv"

	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

// Publish the update only after its fixture exists, under the polling lock.
func (f *Fake) installAndEnqueueFixture(ctx context.Context, value modelFixtureInstall) (int64, error) {
	ctx, cancel := context.WithTimeout(ctx, modelPersistenceTimeout)
	stop := context.AfterFunc(f.modelControl.lifetime, cancel)
	defer func() { stop(); cancel() }()
	if err := f.modelMutationLock(ctx); err != nil {
		return 0, errModelFixtureUnavailable
	}
	defer f.mu.Unlock()
	if value.Input == nil {
		return value.UpdateID, f.installModelCase(ctx, value)
	}
	input := value.Input
	owner, ok := f.domainOwner(input.User)
	if !ok || value.UpdateID != 0 || value.Owner != "" || len(input.Text) == 0 || len(input.Text) > 5000 ||
		len(input.LanguageCode) > 64 {
		return 0, errors.New("invalid fixture input scope")
	}
	if len(f.updates) >= maxPendingUpdates {
		return 0, errors.New("pending update capacity exceeded")
	}
	value.Owner = owner
	value.UpdateID = f.next + 1
	if err := f.installModelCase(ctx, value); err != nil {
		return 0, err
	}
	f.next = value.UpdateID
	message := telegram.Message{
		ID: f.next,
		From: telegram.User{
			ID:           input.User,
			FirstName:    strconv.FormatInt(input.User, 10),
			LanguageCode: input.LanguageCode,
		},
		Chat: telegram.Chat{ID: input.User, Type: privateChat},
		Text: input.Text,
	}
	f.messages = append(f.messages, message)
	f.updates = append(f.updates, telegram.Update{ID: f.next, Message: &message})
	if err := f.save(ctx); err != nil {
		f.modelControl.invalidateInstall(value)
		f.messages = f.messages[:len(f.messages)-1]
		f.updates = f.updates[:len(f.updates)-1]
		return 0, errors.New("fixture update persistence failed")
	}
	return value.UpdateID, nil
}
