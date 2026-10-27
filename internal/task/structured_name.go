package task

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/telegram"
)

func (s *Service) structuredName(
	ctx context.Context,
	t *db.Task,
	c checkpoint,
	i intent,
	scope db.ProfileScope,
	ru bool,
) error {
	if c.ProfileChange == nil {
		mutation, err := s.prepareNameChange(ctx, *t, i, scope)
		if err != nil {
			return s.identityError(ctx, t, c, i, err)
		}
		c.ProfileChange = &mutation
		if err = s.save(ctx, t, c, nil); err != nil {
			return s.wait()
		}
	}
	profile, err := s.store.MutateProfile(ctx, scope, *c.ProfileChange)
	if errors.Is(err, db.ErrConflict) || errors.Is(err, db.ErrInvalidProfile) {
		return s.finishName(
			ctx,
			t,
			c,
			i,
			structuredText(
				ru,
				"The name request changed, expired, or the name is invalid. Open /profile and use /name again.",
				"Запрос имени изменился, истёк или имя некорректно. Открой /profile и отправь /name заново.",
			),
			false,
			ru,
		)
	}
	if err != nil {
		return s.identityError(ctx, t, c, i, err)
	}
	text := structuredText(ru, "I saved your name: ", "Сохранила имя: ") + profile.FullName
	prompt := c.ProfileChange.Kind == "request_name" && profile.NameExpectation != nil
	if prompt {
		text = structuredText(
			ru,
			"Reply to this message with your full name. You can also send /name followed by your full name. Request expires: ",
			"Ответь на это сообщение полным именем. Ещё можно отправить /name и полное имя. Запрос действует до: ",
		) + profile.NameExpectation.ExpiresAt.UTC().
			Format(time.RFC3339)
	}
	return s.finishName(ctx, t, c, i, text, prompt, ru)
}

func (s *Service) prepareNameChange(
	ctx context.Context,
	t db.Task,
	i intent,
	scope db.ProfileScope,
) (db.ProfileMutation, error) {
	m := db.ProfileMutation{OperationID: t.ID}
	p, err := s.store.GetProfile(ctx, scope)
	if err != nil {
		return m, err
	}
	m.ExpectedVersion = p.Version
	m.FullName = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(i.Text), nameCommand))
	m.Kind = "set_name"
	if m.FullName == "" {
		m.Kind = "request_name"
	}
	return m, nil
}

func (s *Service) finishName(
	ctx context.Context,
	t *db.Task,
	c checkpoint,
	i intent,
	text string,
	prompt, ru bool,
) error {
	out := structuredOutput(i, text, nil, ru, "")
	if prompt {
		var payload telegram.SendMessageRequest
		if err := json.Unmarshal(out[len(out)-1].Payload, &payload); err != nil {
			return err
		}
		payload.ReplyMarkup = &telegram.ForceReply{
			ForceReply:            true,
			InputFieldPlaceholder: structuredText(ru, "Full name", "Полное имя"),
		}
		raw, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		out[len(out)-1].Payload = raw
	}
	t.State = stateCompleted
	if err := s.save(ctx, t, c, out); err != nil {
		return s.wait()
	}
	return s.deliver(ctx, t, c)
}
