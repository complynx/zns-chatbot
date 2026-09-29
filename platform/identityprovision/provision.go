// Package identityprovision creates durable Telegram identity bindings. Callers
// must authenticate Telegram updates before calling it; it is not a model tool.
package identityprovision

import (
	"context"
	"errors"
	"net"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrInvalid     = errors.New("identity_provision_invalid")
	ErrConflict    = errors.New("identity_provision_conflict")
	ErrUnavailable = errors.New("identity_provider_unavailable")
	ErrNotFound    = errors.New("identity_provider_not_found")
)

// Provider errors must be sanitized. Get must read the exact immutable ID.
type Provider interface {
	Get(context.Context, string) (Account, error)
	Create(context.Context, Creation) error
}

type Account struct {
	Subject, Organization, Operation string
	Active, Human                    bool
}

// Creation is immutable after its reservation commits. No external OIDC subject
// is implied by the numeric Telegram ID or synthetic email.
type Creation struct {
	Subject, Organization, Operation     string
	FirstName, LastName, Language, Email string
}

type Telegram struct {
	ID                            int64
	FirstName, LastName, Language string
}

type Binding struct{ Owner, Subject string }

type Service struct {
	DB                                *pgxpool.Pool
	Provider                          Provider
	Issuer, Organization, EmailDomain string
	BotID                             int64
	AllowLocalHTTP                    bool
}

// EnsureTelegram grants booking eligibility only when creating a new trusted
// user. Existing imported or revoked policy is never updated. A failed provider
// call leaves a reservation so the next delivery can resume the same identity.
func (s Service) EnsureTelegram(ctx context.Context, input Telegram) (Binding, error) {
	return s.prepare(ctx, input, true)
}

// PrepareTelegram reserves and verifies a provider identity without inserting
// core.users. The removable importer must insert the returned owner and subject
// in its own user/profile/link/receipt transaction, preserving operator policy.
func (s Service) PrepareTelegram(ctx context.Context, input Telegram) (Binding, error) {
	return s.prepare(ctx, input, false)
}

func (s Service) prepare(ctx context.Context, input Telegram, bind bool) (Binding, error) {
	if err := s.validate(input); err != nil {
		return Binding{}, err
	}
	if existing, found, err := s.existing(ctx, input.ID); err != nil || found {
		return existing, err
	}
	if err := s.reserve(ctx, input); err != nil {
		return Binding{}, err
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return Binding{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var binding Binding
	var request Creation
	var ready bool
	err = tx.QueryRow(ctx, `SELECT owner,subject,organization,operation,first_name,last_name,language,email,ready
 FROM core.identity_provisioning WHERE issuer=$1 AND bot_id=$2 AND telegram_id=$3 FOR UPDATE`,
		s.Issuer, s.BotID, input.ID).Scan(&binding.Owner, &request.Subject, &request.Organization, &request.Operation,
		&request.FirstName, &request.LastName, &request.Language, &request.Email, &ready)
	if err != nil {
		return Binding{}, err
	}
	if request.Organization != s.Organization {
		return Binding{}, ErrConflict
	}
	binding.Subject = request.Subject
	if err = s.ensureProvider(ctx, request, ready); err != nil {
		return Binding{}, err
	}
	if bind {
		if err = s.bind(ctx, tx, input.ID, binding, request, ready); err != nil {
			return Binding{}, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return Binding{}, err
	}
	return binding, nil
}

func (s Service) validate(input Telegram) error {
	issuer, err := url.Parse(s.Issuer)
	if err != nil || issuer.Host == "" || issuer.User != nil || issuer.RawQuery != "" || issuer.Fragment != "" ||
		(issuer.Scheme != "https" && !s.localHTTP(issuer)) {
		return ErrInvalid
	}
	if s.DB == nil || s.Provider == nil || s.Organization == "" || s.BotID <= 0 || s.BotID >= 1<<52 ||
		input.ID <= 0 || input.ID >= 1<<52 || !strings.HasSuffix(s.EmailDomain, ".invalid") ||
		strings.ContainsAny(s.EmailDomain, " /:@\\\r\n") || utf8.RuneCountInString(input.FirstName) > 200 ||
		utf8.RuneCountInString(input.LastName) > 200 || len(input.Language) > 10 {
		return ErrInvalid
	}
	return nil
}

func (s Service) localHTTP(issuer *url.URL) bool {
	ip := net.ParseIP(issuer.Hostname())
	return s.AllowLocalHTTP && issuer.Scheme == "http" &&
		(issuer.Hostname() == "localhost" || ip != nil && ip.IsLoopback())
}

func (s Service) ensureProvider(ctx context.Context, request Creation, ready bool) error {
	account, err := s.Provider.Get(ctx, request.Subject)
	if errors.Is(err, ErrNotFound) && !ready {
		// Any outcome can follow a successful remote commit. Read the reserved ID
		// even when Create returns FailedPrecondition or a lost-response error.
		createErr := s.Provider.Create(ctx, request)
		account, err = s.Provider.Get(ctx, request.Subject)
		if errors.Is(err, ErrNotFound) && createErr != nil {
			return createErr
		}
	}
	if err != nil {
		return err
	}
	if account.Subject != request.Subject || account.Organization != request.Organization ||
		account.Operation != request.Operation || !account.Human || !account.Active {
		return ErrConflict
	}
	return nil
}

func (s Service) existing(ctx context.Context, telegramID int64) (Binding, bool, error) {
	var result Binding
	var issuer string
	var active bool
	err := s.DB.QueryRow(ctx, `SELECT u.id,COALESCE(z.subject,''),COALESCE(z.issuer,''),COALESCE(z.active,false)
 FROM core.users u LEFT JOIN core.zitadel_identities z ON z.owner=u.id
 WHERE u.telegram_id=$1`, telegramID).
		Scan(&result.Owner, &result.Subject, &issuer, &active)
	if errors.Is(err, pgx.ErrNoRows) {
		return Binding{}, false, nil
	}
	if err != nil {
		return Binding{}, false, err
	}
	var mapped bool
	err = s.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM core.telegram_identities WHERE bot_id=$1 AND telegram_id=$2 AND owner=$3)`,
		s.BotID, telegramID, result.Owner).
		Scan(&mapped)
	if err != nil {
		return Binding{}, false, err
	}
	if !mapped || !active || issuer != s.Issuer {
		return Binding{}, true, ErrConflict
	}
	var mismatch bool
	err = s.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM core.identity_provisioning
 WHERE issuer=$1 AND bot_id=$2 AND telegram_id=$3 AND (owner<>$4 OR subject<>$5 OR organization<>$6))`,
		s.Issuer, s.BotID, telegramID, result.Owner, result.Subject, s.Organization).Scan(&mismatch)
	if err != nil {
		return Binding{}, true, err
	}
	if mismatch {
		return Binding{}, true, ErrConflict
	}
	account, err := s.Provider.Get(ctx, result.Subject)
	if err != nil {
		return Binding{}, true, err
	}
	if account.Subject != result.Subject || account.Organization != s.Organization || !account.Active ||
		!account.Human {
		return Binding{}, true, ErrConflict
	}
	return result, true, nil
}

func (s Service) reserve(ctx context.Context, input Telegram) error {
	first, last := strings.TrimSpace(input.FirstName), strings.TrimSpace(input.LastName)
	if first == "" {
		first = "Telegram"
	}
	email := "tg+" + strconv.FormatInt(s.BotID, 10) + "+" + strconv.FormatInt(input.ID, 10) + "@" + s.EmailDomain
	_, err := s.DB.Exec(
		ctx,
		`INSERT INTO core.identity_provisioning
 (issuer,organization,bot_id,telegram_id,owner,subject,operation,first_name,last_name,language,email)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) ON CONFLICT(issuer,bot_id,telegram_id) DO NOTHING`,
		s.Issuer,
		s.Organization,
		s.BotID,
		input.ID,
		uuid.NewString(),
		"zns-"+uuid.NewString(),
		uuid.NewString(),
		first,
		last,
		input.Language,
		email,
	)
	return err
}
