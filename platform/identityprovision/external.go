package identityprovision

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
)

type ExternalIdentity struct{ IDP, Subject string }

type ExternalProvider interface {
	Links(context.Context, string) ([]ExternalIdentity, error)
	AddLink(context.Context, string, ExternalIdentity) error
}

// ExternalLinker accepts the exact subject from a verified authorizer assertion.
// Its configured IDP is never chosen by the request or inferred from an email.
type ExternalLinker struct {
	Provisioner Service
	Provider    ExternalProvider
	IDP         string
}

func (s ExternalLinker) EnsureExternal(ctx context.Context, input Telegram, externalSubject string) (Binding, error) {
	if s.Provider == nil || strings.TrimSpace(s.IDP) == "" || externalSubject == "" || len(externalSubject) > 1024 {
		return Binding{}, ErrInvalid
	}
	binding, err := s.Provisioner.EnsureTelegram(ctx, input)
	if err != nil {
		return Binding{}, err
	}
	_, err = s.Provisioner.DB.Exec(ctx, `INSERT INTO core.identity_external_links
 (issuer,bot_id,telegram_id,idp_id,external_subject,owner,subject) VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT DO NOTHING`,
		s.Provisioner.Issuer, s.Provisioner.BotID, input.ID, s.IDP, externalSubject, binding.Owner, binding.Subject)
	if err != nil {
		return Binding{}, err
	}
	if err = s.linkReserved(ctx, input.ID, binding, externalSubject); err != nil {
		return Binding{}, err
	}
	return binding, nil
}

func (s ExternalLinker) linkReserved(
	ctx context.Context,
	telegramID int64,
	binding Binding,
	externalSubject string,
) error {
	tx, err := s.Provisioner.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var reserved Binding
	var subject string
	var ready bool
	err = tx.QueryRow(
		ctx,
		`SELECT owner,subject,external_subject,ready FROM core.identity_external_links
 WHERE issuer=$1 AND bot_id=$2 AND telegram_id=$3 AND idp_id=$4 FOR UPDATE`,
		s.Provisioner.Issuer,
		s.Provisioner.BotID,
		telegramID,
		s.IDP,
	).Scan(&reserved.Owner, &reserved.Subject, &subject, &ready)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrConflict
	}
	if err != nil {
		return err
	}
	if reserved != binding || subject != externalSubject {
		return ErrConflict
	}
	if err = s.ensureLink(ctx, binding.Subject, ExternalIdentity{IDP: s.IDP, Subject: subject}, ready); err != nil {
		return err
	}
	_, err = tx.Exec(
		ctx,
		`UPDATE core.identity_external_links SET ready=true WHERE issuer=$1 AND bot_id=$2 AND telegram_id=$3 AND idp_id=$4`,
		s.Provisioner.Issuer,
		s.Provisioner.BotID,
		telegramID,
		s.IDP,
	)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s ExternalLinker) ensureLink(ctx context.Context, subject string, wanted ExternalIdentity, ready bool) error {
	links, err := s.Provider.Links(ctx, subject)
	if err != nil {
		return err
	}
	found, err := matchesExternal(links, wanted)
	if err != nil || found {
		return err
	}
	if ready {
		return ErrConflict
	}
	createErr := s.Provider.AddLink(ctx, subject, wanted)
	links, err = s.Provider.Links(ctx, subject)
	if err != nil {
		return err
	}
	found, err = matchesExternal(links, wanted)
	if err != nil || found {
		return err
	}
	if createErr != nil {
		return createErr
	}
	return ErrConflict
}

func matchesExternal(links []ExternalIdentity, wanted ExternalIdentity) (bool, error) {
	found := false
	for _, link := range links {
		if link.IDP != wanted.IDP {
			continue
		}
		if link.Subject != wanted.Subject {
			return false, ErrConflict
		}
		found = true
	}
	return found, nil
}
