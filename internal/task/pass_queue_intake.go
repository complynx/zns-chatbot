package task

import (
	"encoding/json"
	"strconv"

	"github.com/google/uuid"

	"github.com/complynx/zns-chatbot/internal/db"
)

const passModePair = "pair"

func (s *Service) passSlugAtIntake(i intent) *db.PassSlugBinding {
	input := i.PassInput
	if input == nil || input.Event == "" || input.Invalid || input.DetailsDisabled {
		return nil
	}
	if id, err := uuid.Parse(input.Event); err == nil && id.String() == input.Event {
		return nil
	}
	return &db.PassSlugBinding{Organization: s.notificationScope().Organization, Slug: input.Event,
		Bind: func(r *db.IntakeRequest, id uuid.UUID) error {
			switch {
			case id != uuid.Nil:
				input.Event = id.String()
			case input.Code == "" && input.Mode == "":
				// A bare argument that was not an event is an invitation code.
				input.Code, input.Event = input.Event, ""
			default:
				input.Invalid = true
			}
			r.PassTicket = s.passTicketAtIntake(i)
			r.PassInvite = s.passInviteAtIntake(i)
			payload, err := json.Marshal(i)
			r.Payload = payload
			return err
		},
	}
}

func (s *Service) passTicketAtIntake(i intent) *db.PassTicketInput {
	if i.PassCallback != "" {
		index, err := strconv.Atoi(i.PassCallback)
		if err != nil || index < 0 {
			return nil
		}
		return &db.PassTicketInput{
			Organization: s.notificationScope().Organization,
			PromptID:     i.CallbackTaskID,
			MessageID:    i.CallbackMessageID,
			Callback:     passCallbackPrefix + i.CallbackTaskID.String() + ":" + i.PassCallback,
			Choice:       index,
		}
	}
	if i.PassInput == nil || i.PassInput.Invalid || i.PassInput.DetailsDisabled || i.PassInput.Event == "" ||
		i.PassInput.Code != "" {
		return nil
	}
	return &db.PassTicketInput{
		Organization: s.notificationScope().Organization,
		Event:        i.PassInput.Event,
		Role:         i.PassInput.Role,
		PairMode:     i.PassInput.Mode == passModePair,
	}
}
