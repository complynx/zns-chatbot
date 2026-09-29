// Package registrationnative binds native callbacks to immutable intake evidence.
package registrationnative

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
	"github.com/complynx/zns-chatbot/platform/internal/registrationingress"
	"github.com/complynx/zns-chatbot/platform/internal/registrationnative/dbgen"
)

type Envelope struct {
	Owner    string                 `json:"owner"`
	Chat     int64                  `json:"chat"`
	Token    string                 `json:"token"`
	Revision int64                  `json:"revision"`
	Command  passbooking.Command    `json:"command"`
	Source   *readsource.Derivation `json:"source,omitempty"`
}

// Classify reads only saved transport bindings. It takes no row locks and does
// not authenticate remotely while the inbox transaction is open.
func Classify(ctx context.Context, tx pgx.Tx, chat int64, token string) (Envelope, bool, error) {
	if chat <= 0 || token == "" || len(token) > 128 {
		return Envelope{}, false, nil
	}
	rows, err := dbgen.New(tx).NativeBindings(ctx, dbgen.NativeBindingsParams{ChatID: chat, Token: token})
	if err != nil {
		return Envelope{}, false, err
	}
	if len(rows) != 1 {
		return Envelope{}, false, nil
	}
	row := rows[0]
	return decode(row.Action, row.State, row.Owner, chat, token, row.Revision)
}

func decode(action, state []byte, owner string, chat int64, token string, revision int64) (Envelope, bool, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(action, &fields); err != nil {
		return Envelope{}, false, err
	}
	raw, ok := fields["command"]
	if !ok || len(fields) != 1 || bytes.Equal(raw, []byte("null")) {
		return Envelope{}, false, nil
	}
	var command passbooking.Command
	if err := json.Unmarshal(raw, &command); err != nil {
		return Envelope{}, false, err
	}
	if !passbooking.InitiatesRegistration(command) || command.Event == "" || command.Version < 0 {
		return Envelope{}, false, nil
	}
	if command.Name == "invite" && (command.InviteTelegramID <= 0 || command.InviteTelegramID == chat) {
		return Envelope{}, false, nil
	}
	var menu struct {
		Source   *readsource.Derivation `json:"source"`
		Redacted bool                   `json:"redacted"`
	}
	if err := json.Unmarshal(state, &menu); err != nil {
		return Envelope{}, false, err
	}
	if menu.Redacted || (menu.Source != nil && !menu.Source.Valid()) {
		return Envelope{}, false, nil
	}
	command.Key = "passmenu-" + token
	return Envelope{
		Owner:    owner,
		Chat:     chat,
		Token:    token,
		Revision: revision,
		Command:  command,
		Source:   menu.Source,
	}, true, nil
}

func (e Envelope) Binding() (*registrationingress.NativeBinding, error) {
	payload, err := json.Marshal(e)
	if err != nil {
		return nil, err
	}
	return &registrationingress.NativeBinding{Event: e.Command.Event, Owner: e.Owner, Payload: payload}, nil
}

// Check holds the exact metadata binding through the caller's domain commit.
// Current identity and source fences are the app resolver's responsibility.
func Check(ctx context.Context, tx pgx.Tx, e Envelope) (bool, error) {
	row, err := dbgen.New(tx).
		LockNativeBinding(ctx, dbgen.LockNativeBindingParams{Owner: e.Owner, ChatID: e.Chat, Token: e.Token})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	current, found, err := decode(row.Action, row.State, row.Owner, e.Chat, e.Token, row.Revision)
	if err != nil || !found {
		return false, err
	}
	if current.Owner != e.Owner || current.Revision != e.Revision || current.Command != e.Command {
		return false, nil
	}
	before, err := json.Marshal(e.Source)
	if err != nil {
		return false, err
	}
	after, err := json.Marshal(current.Source)
	return bytes.Equal(before, after), err
}
