package botdelivery

import (
	"context"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/interaction"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

// PassMenu retains the private source outside the model-visible menu contract.
type PassMenu struct {
	interaction.RegistrationMenu

	Source   *readsource.Derivation `json:"source,omitempty"`
	Redacted bool                   `json:"redacted,omitempty"`
}
type SourceRequest struct {
	Owner  string
	Source *readsource.Derivation
}
type PassMenuRequest struct {
	Owner          string
	Chat, Revision int64
	State          interaction.RegistrationMenu
	Source         *readsource.Derivation
}

func (s Service) CheckSource(ctx context.Context, in SourceRequest) error {
	if in.Owner == "" || in.Source == nil {
		return ErrBinding
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return core.DatabaseOperationError(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	return lockDerivation(ctx, tx, in.Owner, *in.Source)
}
func (s Service) StorePassMenu(ctx context.Context, in PassMenuRequest) error {
	if in.Owner == "" || in.Chat <= 0 || in.Revision < 0 {
		return ErrBinding
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return core.DatabaseOperationError(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	ref := Reference{Kind: CardIntent, Family: familyStatic, CardKey: familyPasses, Source: in.Source}
	if in.Source != nil {
		ref.Generation = in.Source.Generation
	}
	if err = s.lockSource(ctx, tx, Intent{Owner: in.Owner, Chat: in.Chat, Reference: ref}); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO bot.pass_views(owner,chat_id,revision,state) VALUES($1,$2,$3,$4)
 ON CONFLICT(owner) DO UPDATE SET chat_id=$2,revision=$3,state=$4 WHERE bot.pass_views.revision<$3 OR (bot.pass_views.revision=$3 AND NOT COALESCE((bot.pass_views.state->>'redacted')::boolean,false))`, in.Owner, in.Chat, in.Revision, PassMenu{RegistrationMenu: in.State, Source: in.Source})
	if err != nil {
		return core.DatabaseOperationError(err)
	}
	return core.DatabaseOperationError(tx.Commit(ctx))
}
