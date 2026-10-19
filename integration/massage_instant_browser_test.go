//go:build browserqa

package integration_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/massage"
)

// This explicit browserqa gate uses real shipped HTML/JS, HTTP and PostgreSQL.
// IAM, Loom and Telegram SDK are test peers; the driver must block other origins.
func TestMassageInstantBrowserNativeAndHandoffs(t *testing.T) {
	t.Parallel()
	node, driver, output := os.Getenv(
		"ZINUSYA_QA_NODE",
	), os.Getenv(
		"ZINUSYA_QA_BROWSER_DRIVER",
	), os.Getenv(
		"ZINUSYA_QA_DIR",
	)
	require.NotEmpty(t, node)
	require.NotEmpty(t, driver)
	require.NotEmpty(t, output)
	public := httptest.NewUnstartedServer(http.NotFoundHandler())
	t.Cleanup(public.Close)
	p := newPeers(t)
	p.miniAppOrigin = "https://" + public.Listener.Addr().String()
	f, _, _, schedule := massageBookingFixtureWithPeers(t, 1, p)
	user, token := registrationUser(t, f.H, 11444, false)
	admin := ownerProfileScope(t, f.H, f.AdminID)
	bindingChange(t, f, admin, db.MassageBindingChange{
		EventID: f.Event.ID, SpecialistID: "alex", UserID: user, Active: true,
		ExpectedEventVersion: f.Event.Version, ExpectedScheduleVersion: schedule.ExpectedVersion,
	})
	schedule.Document.Services = append(schedule.Document.Services, massage.ScheduleService{
		ID: "long", Name: massage.ScheduleLabel{EN: "Long", RU: "Длинный"}, Blocks: 6, PriceMinor: 27000,
	})
	schedule.Document.Specialists[0].ServiceIDs = append(schedule.Document.Specialists[0].ServiceIDs, "long")
	args := moveMassagePartyToNow(t, &f, admin, schedule)
	public.Config.Handler = f.H.endpoint.PublicHandler()
	public.StartTLS()
	internal := httptest.NewServer(f.H.endpoint.InternalHandler())
	t.Cleanup(internal.Close)
	config := map[string]any{
		"origin": public.URL, "internal": internal.URL, "token": token,
		"initData": appProof(11444), "owner": user, "args": args, "output": output,
	}
	raw, err := json.Marshal(config)
	require.NoError(t, err)
	configFile := filepath.Join(t.TempDir(), "browser.private.json")
	require.NoError(t, os.WriteFile(configFile, raw, 0o600))
	ctx, cancel := context.WithTimeout(t.Context(), 150*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, node, driver, configFile)
	log, runErr := command.CombinedOutput()
	require.NoError(t, os.WriteFile(filepath.Join(output, "browser-driver.private.log"), log, 0o600))
	var ledger []byte
	require.NoError(t, f.H.store.Pool().QueryRow(t.Context(), `SELECT jsonb_build_object(
 'bookings',(SELECT coalesce(jsonb_agg(to_jsonb(t)),'[]'::jsonb) FROM shuttle_massage_bookings t),
 'preparations',(SELECT coalesce(jsonb_agg(to_jsonb(t)),'[]'::jsonb) FROM shuttle_massage_preparations t),
 'operations',(SELECT coalesce(jsonb_agg(to_jsonb(t)),'[]'::jsonb) FROM shuttle_massage_operations t),
 'reminders',(SELECT coalesce(jsonb_agg(to_jsonb(t)),'[]'::jsonb) FROM shuttle_massage_reminders t),
 'notices',(SELECT coalesce(jsonb_agg(to_jsonb(t)),'[]'::jsonb) FROM shuttle_massage_specialist_notifications t))`).Scan(&ledger))
	require.NoError(t, os.WriteFile(filepath.Join(output, "database.private.json"), ledger, 0o600))
	require.NoError(t, runErr, "Inspect the retained browser log; do not replay uncertain effects")
	var booked, cancelled, preparations, tasks, notices int
	require.NoError(t, f.H.store.Pool().QueryRow(t.Context(),
		"SELECT count(*) FILTER(WHERE state='booked'),count(*) FILTER(WHERE state='cancelled') FROM shuttle_massage_bookings").
		Scan(&booked, &cancelled))
	require.Equal(t, 1, booked)
	require.Equal(t, 2, cancelled)
	var canonical int
	arguments, err := json.Marshal(args)
	require.NoError(t, err)
	require.NoError(t, f.H.store.Pool().QueryRow(t.Context(), `SELECT count(*)
 FROM shuttle_massage_bookings b JOIN shuttle_massage_preparations p ON p.id=b.preparation_id
 WHERE p.kind='instant' AND p.user_id=$1 AND b.user_id=$1
 AND p.snapshot=b.snapshot AND p.arguments=$2::jsonb`, user, arguments).Scan(&canonical))
	require.Equal(t, 3, canonical, "Every booking retains the original owner's exact frozen terms")

	require.NoError(
		t,
		f.H.store.Pool().QueryRow(t.Context(), "SELECT count(*) FROM shuttle_massage_preparations").Scan(&preparations),
	)
	require.Equal(t, 5, preparations, "Handoff/adoption and recovery must not create replacement proposals")
	require.NoError(t, f.H.store.Pool().QueryRow(t.Context(), "SELECT count(*) FROM shuttle_tasks").Scan(&tasks))
	require.Zero(t, tasks)
	require.NoError(t, f.H.store.Pool().QueryRow(t.Context(),
		"SELECT count(*) FROM shuttle_massage_specialist_notifications WHERE kind='booked'").Scan(&notices))
	require.Zero(t, notices)
	page, err := f.H.store.OwnPendingMassageProposals(
		t.Context(),
		ownerProfileScope(t, f.H, user),
		args.EventID,
		uuid.Nil,
		20,
	)
	require.NoError(t, err)
	require.Empty(t, page.Proposals)
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Empty(t, p.runs, "Native UI and direct agent HTTP do not launch an agent cycle")
}
