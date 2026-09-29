package migrate_test

import (
	"testing"

	migrate "github.com/complynx/zns-chatbot/tools/migrate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func addOrderRUAdmin(t *testing.T, dir string, m *migrate.Manifest) {
	t.Helper()
	changeOrderSource(t, dir, m, "users.jsonl", `"print_name":"Synthetic"}`, `"print_name":"Synthetic"}`+"\n"+
		`{"_id":"user-ru","bot_id":77,"user_id":202,"print_name":"Synthetic RU"}`)
	for i := range m.Files {
		if m.Files[i].Path == "users.jsonl" {
			m.Files[i].Records++
		}
	}
	changeOrderSource(t, dir, m, "configuration.jsonl", `"payment_admins":[`,
		`"payment_admin_ru":202,"payment_admins":[{"user_id":202,"country":"ru","region":"Synthetic RU"},`)
}

func TestApplyOrdersResolvesEffectiveRUAdminWithoutChangingSource(t *testing.T) {
	t.Parallel()
	for _, state := range []string{"proof", "paid", "unrouted"} {
		t.Run(state, func(t *testing.T) {
			t.Parallel()
			dsn, db := applyDatabase(t)
			stage, plan, resolutions := orderImportInputs(t, false, addOrderRUAdmin,
				func(t *testing.T, dir string, m *migrate.Manifest) {
					if state != "unrouted" {
						changeOrderSource(t, dir, m, "orders.jsonl", `"proof_file":"synthetic-file"`,
							`"proof_file":"synthetic-file","proof_country":"ru"`)
					}
					if state == "proof" {
						changeOrderSource(t, dir, m, "orders.jsonl", `"validation":true`, `"validation":false`)
					}
				})
			applyOrderDependencies(t, dsn, stage)
			_, err := migrate.ApplyOrders(t.Context(), dsn, stage, plan, resolutions, migrate.DefaultLimits())
			require.NoError(t, err)
			var admin, country, gotState string
			require.NoError(t, db.QueryRow(t.Context(), `SELECT payment_admin,country,state FROM core.orders
			WHERE id='legacy-order:77:000000000000000000000001'`).Scan(&admin, &country, &gotState))
			if state == "unrouted" {
				assert.Empty(t, admin)
				assert.Empty(t, country)
				assert.Equal(t, "paid", gotState)
			} else {
				assert.Equal(t, "owner-202", admin)
				assert.Equal(t, "ru", country)
				assert.Equal(t, state, gotState)
			}
			var sourceHasAdmin bool
			require.NoError(t, db.QueryRow(t.Context(), `SELECT source_record ? 'proof_admin'
			FROM core.legacy_order_import_references WHERE source_domain='orders'
			AND target_id='legacy-order:77:000000000000000000000001'`).Scan(&sourceHasAdmin))
			assert.False(t, sourceHasAdmin)
			require.NoError(t, db.QueryRow(t.Context(), `SELECT payment_admin FROM core.orders
			WHERE id='legacy-order:77:000000000000000000000003'`).Scan(&admin))
			assert.Equal(t, "owner-101", admin)
			_, err = migrate.ApplyOrders(t.Context(), dsn, stage, plan, resolutions, migrate.DefaultLimits())
			require.NoError(t, err)
			_, err = migrate.ReconcileOrders(t.Context(), dsn, stage, plan, resolutions, migrate.DefaultLimits())
			require.NoError(t, err)
			if state == "unrouted" {
				return
			}
			_, err = db.Exec(t.Context(), `UPDATE core.orders SET payment_admin='' WHERE country='ru'`)
			require.NoError(t, err)
			_, err = migrate.ReconcileOrders(t.Context(), dsn, stage, plan, resolutions, migrate.DefaultLimits())
			require.EqualError(t, err, "apply_reconciliation_failed")
		})
	}
}

func TestApplyOrdersRefusesUnresolvedEffectiveRUAdmin(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, path, from, to, code string }{
		{"disabled", "configuration.jsonl", `"payment_admin_ru":202`, `"payment_admin_ru":0`, "order_implicit_admin_mapping_required"},
		{"missing", "configuration.jsonl", `"payment_admin_ru":202,`, ``, "order_implicit_admin_mapping_required"},
		{"not exported admin", "configuration.jsonl", `"payment_admin_ru":202`, `"payment_admin_ru":303`, "order_ru_admin_dependency_required"},
		{"wrong country", "configuration.jsonl", `"payment_admin_ru":202`, `"payment_admin_ru":101`, "order_ru_admin_dependency_required"},
		{"other bot", "users.jsonl", `"bot_id":77,"user_id":202`, `"bot_id":88,"user_id":202`, "order_owner_dependency_required"},
		{"null", "configuration.jsonl", `"payment_admin_ru":202`, `"payment_admin_ru":null`, "apply_record_blocked"},
		{"negative", "configuration.jsonl", `"payment_admin_ru":202`, `"payment_admin_ru":-1`, "apply_record_blocked"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			stage, plan, resolutions := orderImportInputs(t, false, addOrderRUAdmin,
				func(t *testing.T, dir string, m *migrate.Manifest) {
					changeOrderSource(t, dir, m, "orders.jsonl", `"proof_file":"synthetic-file"`,
						`"proof_file":"synthetic-file","proof_country":"ru"`)
					changeOrderSource(t, dir, m, tc.path, tc.from, tc.to)
				})
			_, err := migrate.ApplyOrders(
				t.Context(), "invalid database", stage, plan, resolutions, migrate.DefaultLimits(),
			)
			require.EqualError(t, err, tc.code)
		})
	}
}
