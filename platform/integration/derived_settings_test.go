package integration_test

import (
	"context"
	"testing"
	"time"

	"github.com/complynx/zns-chatbot/platform/internal/account"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/credits"
	"github.com/complynx/zns-chatbot/platform/internal/derivedmutation"
	knowledgeauthority "github.com/complynx/zns-chatbot/platform/internal/knowledge/authority"
	"github.com/complynx/zns-chatbot/platform/internal/modelsettings"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

func derivedSettingsFixture(t *testing.T) (*pgxpool.Pool, derivedmutation.Service, readsource.Derivation) {
	t.Helper()
	db, _ := bookingFixture(t)
	_, err := db.Exec(
		t.Context(),
		`INSERT INTO core.knowledge_permissions(scope,actor,permission) VALUES('','bob','review') ON CONFLICT DO NOTHING`,
	)
	require.NoError(t, err)
	generation := int64(0)
	return db, derivedmutation.Service{
			DB:            db,
			Account:       account.Service{DB: db},
			ModelSettings: modelsettings.Service{DB: db},
			Credits:       credits.Service{DB: db},
		},
		readsource.Derivation{
			Generation: &generation,
			Authorities: []readsource.Authority{
				{Knowledge: knowledgeauthority.ReadAuthority{Kind: knowledgeauthority.Review}},
			},
		}
}

func TestDerivedSettingsSourceRevocationAndReceipts(t *testing.T) {
	t.Parallel()
	for _, domain := range []string{"language", "model", "credit-default", "credit-user", "grant"} {
		t.Run(domain, func(t *testing.T) {
			t.Parallel()
			db, service, source := derivedSettingsFixture(t)
			call := func(key string) error {
				switch domain {
				case "language":
					_, err := service.SetLanguage(
						t.Context(),
						"bob",
						account.LanguageChange{Language: "en", OperationKey: account.LanguageOperationKey(key)},
						source,
					)
					return err
				case "model":
					_, err := service.SetModelSettings(
						t.Context(),
						"bob",
						"alice",
						modelsettings.Change{Model: "gpt-6-sol", Effort: "high", OperationKey: key},
						source,
					)
					return err
				case "grant":
					return service.GrantModelSettings(
						t.Context(),
						"bob",
						modelsettings.Grant{
							Owner:        "alice",
							Capability:   modelsettings.Own,
							Enabled:      true,
							OperationKey: key,
						},
						source,
					)
				default:
					payer := "alice"
					if domain == "credit-default" {
						payer = "*"
					}
					amount := int64(42)
					_, err := service.SetCreditPolicy(
						t.Context(),
						"bob",
						payer,
						credits.PolicyChange{MonthlyNanoUSD: &amount, Version: 1, OperationKey: key},
						source,
					)
					return err
				}
			}
			require.NoError(t, call("saved"))
			_, err := db.Exec(t.Context(), `DELETE FROM core.knowledge_permissions WHERE actor='bob'`)
			require.NoError(t, err)
			require.NoError(t, call("saved"), "completed receipt survives source revocation")
			requireCode(t, call("new"), "source_stale")
			queries := map[string]string{
				"language":       `SELECT count(*) FROM core.language_operations WHERE owner='bob'`,
				"model":          `SELECT count(*) FROM core.model_setting_operations WHERE actor='bob'`,
				"grant":          `SELECT count(*) FROM core.model_grant_operations WHERE actor='bob'`,
				"credit-default": `SELECT count(*) FROM credits.policy_changes WHERE actor='bob'`,
				"credit-user":    `SELECT count(*) FROM credits.policy_changes WHERE actor='bob'`,
			}
			var receipts int
			require.NoError(t, db.QueryRow(t.Context(), queries[domain]).Scan(&receipts))
			require.Equal(t, 1, receipts, "replay and denied new effect must not add receipts")
			if domain != "language" {
				_, err = db.Exec(t.Context(), `DELETE FROM core.pass_booking_admins WHERE owner='bob'`)
				require.NoError(t, err)
				requireCode(t, call("saved"), "forbidden")
			}
		})
	}
}

func TestDerivedGrantReceiptCannotRestoreLaterRevocation(t *testing.T) {
	t.Parallel()
	db, service, source := derivedSettingsFixture(t)
	input := modelsettings.Grant{
		Owner:        "alice",
		Capability:   modelsettings.Own,
		Enabled:      true,
		OperationKey: "derived-grant",
	}
	require.NoError(t, service.GrantModelSettings(t.Context(), "bob", input, source))
	require.NoError(
		t,
		service.ModelSettings.Grant(
			t.Context(),
			"bob",
			modelsettings.Grant{Owner: "alice", Capability: modelsettings.Own, Enabled: false},
		),
	)
	_, err := db.Exec(t.Context(), `DELETE FROM core.knowledge_permissions WHERE actor='bob'`)
	require.NoError(t, err)
	require.NoError(t, service.GrantModelSettings(t.Context(), "bob", input, source))
	var count int
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT count(*) FROM core.model_setting_grants WHERE owner='alice' AND capability='own'`).
			Scan(&count),
	)
	require.Zero(t, count)
	input.Enabled = false
	requireCode(t, service.GrantModelSettings(t.Context(), "bob", input, source), "operation_conflict")
	_, err = db.Exec(t.Context(), `DELETE FROM core.pass_booking_admins WHERE owner='bob'`)
	require.NoError(t, err)
	input.Enabled = true
	requireCode(t, service.GrantModelSettings(t.Context(), "bob", input, source), "forbidden")
}

func TestDerivedModelRevocationWinsAtDomainBarrier(t *testing.T) {
	t.Parallel()
	db, service, source := derivedSettingsFixture(t)
	tx, err := db.Begin(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback(context.WithoutCancel(t.Context())) })
	var pid int32
	require.NoError(t, tx.QueryRow(t.Context(), `SELECT pg_backend_pid()`).Scan(&pid))
	_, err = tx.Exec(t.Context(), `SELECT pg_advisory_xact_lock(481048)`)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, callErr := service.SetModelSettings(
			ctx,
			"bob",
			"alice",
			modelsettings.Change{Model: "gpt-6-sol", Effort: "high", OperationKey: "blocked-model"},
			source,
		)
		done <- callErr
	}()
	waitMutationBlocked(t, db, pid, 1)
	_, err = db.Exec(ctx, `DELETE FROM core.knowledge_permissions WHERE actor='bob'`)
	require.NoError(t, err)
	require.NoError(t, tx.Commit(ctx))
	requireCode(t, <-done, "source_stale")
	var count int
	require.NoError(
		t,
		db.QueryRow(ctx, `SELECT count(*) FROM core.model_setting_operations WHERE operation_key='blocked-model'`).
			Scan(&count),
	)
	require.Zero(t, count)
}

func TestDerivedCreditDenialDoesNotMaterializeAccount(t *testing.T) {
	t.Parallel()
	db, service, source := derivedSettingsFixture(t)
	_, err := db.Exec(t.Context(), `DELETE FROM core.knowledge_permissions WHERE actor='bob'`)
	require.NoError(t, err)
	amount := int64(42)
	_, err = service.SetCreditPolicy(
		t.Context(),
		"bob",
		"alice",
		credits.PolicyChange{MonthlyNanoUSD: &amount, Version: 1, OperationKey: "denied-account"},
		source,
	)
	requireCode(t, err, "source_stale")
	var count int
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT count(*) FROM credits.accounts WHERE payer='alice'`).Scan(&count),
	)
	require.Zero(t, count)
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT count(*) FROM credits.policy_changes WHERE operation_key='denied-account'`).
			Scan(&count),
	)
	require.Zero(t, count)
}
