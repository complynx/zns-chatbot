package migrate_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	migrate "github.com/complynx/zns-chatbot/tools/migrate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOrderPrerequisitePlanPreservesBlockedEvidence(t *testing.T) {
	t.Parallel()
	directory, manifest := snapshot(t)
	order := []byte(
		`{"_id":"order-one","user_id":42,"event_key":"event","choice":{"total":12.34,"extras":{"shuttle":65}},"proof_file":"cash","validation":true,"unknown_active":{"value":null}}` + "\n",
	)
	food := []byte(
		`{"_id":"food-one","pass_key":"old-event","payment_status":"paid","activities":{"yoga":true}}` + "\n",
	)
	capacity := []byte(
		`{"_id":"event:shuttle:0","event_key":"event","service":"shuttle","seat":0,"reservation_id":"order-one","payment_attempt_token":"attempt"}` + "\n",
	)
	addFile(t, directory, &manifest, "orders.jsonl", "orders", "records", append(order, food...), 2)
	addFile(t, directory, &manifest, "capacity.jsonl", "order_capacity", "records", capacity, 1)
	addFile(t, directory, &manifest, "menu.json", "configuration", "resource", []byte(`{"private":"configuration"}`), 0)
	writeManifest(t, directory, manifest)
	stage := filepath.Join(t.TempDir(), "stage")
	_, _, err := migrate.Stage(directory, stage, migrate.DefaultLimits())
	require.NoError(t, err)
	target := filepath.Join(t.TempDir(), "orders.json")
	summary, err := migrate.PlanOrders(stage, target, migrate.DefaultLimits())
	require.NoError(t, err)
	assert.False(t, summary.ApplyReady)
	assert.EqualValues(t, 3, summary.Blocked)
	raw, err := os.ReadFile(target)
	require.NoError(t, err)
	var plan migrate.OrderPlan
	require.NoError(t, json.Unmarshal(raw, &plan))
	require.Len(t, plan.Records, 3)
	require.Len(t, plan.Files, 3)
	assert.JSONEq(t, string(order), string(plan.Records[0].Record))
	assert.JSONEq(t, string(food), string(plan.Records[1].Record))
	assert.JSONEq(t, string(capacity), string(plan.Records[2].Record))
	assert.Equal(t, []string{"legacy_food_mapping_required"}, plan.Records[1].Blockers)
	assert.Equal(t, []string{"order_slot_field_unmapped"}, plan.Records[2].Blockers)
	assert.NotEqual(t, plan.Records[0].Legacy.Key, plan.Records[1].Legacy.Key)
	summary, err = migrate.PlanOrders(stage, target, migrate.DefaultLimits())
	require.NoError(t, err)
	assert.True(t, summary.Reused)
	require.NoError(t, os.WriteFile(target, []byte("{}"), 0o600))
	_, err = migrate.PlanOrders(stage, target, migrate.DefaultLimits())
	require.EqualError(t, err, "plan_replay_mismatch")
}

func TestOrderPrerequisiteRequiresSource(t *testing.T) {
	t.Parallel()
	directory, _ := snapshot(t)
	stage := filepath.Join(t.TempDir(), "stage")
	_, _, err := migrate.Stage(directory, stage, migrate.DefaultLimits())
	require.NoError(t, err)
	target := filepath.Join(t.TempDir(), "orders.json")
	_, err = migrate.PlanOrders(stage, target, migrate.DefaultLimits())
	require.EqualError(t, err, "orders_source_required")
	_, err = os.Stat(target)
	require.ErrorIs(t, err, os.ErrNotExist)
}
