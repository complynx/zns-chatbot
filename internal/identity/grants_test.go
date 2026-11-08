//nolint:testpackage // Verify fixed IAM wire contracts and cached token invalidation.
package identity

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/telegram"
)

// iamFixture holds its mutex while serving this state.
type initialGrantFixture struct {
	values        map[string]loomUserGrant
	writes        int
	reads         int
	status        int
	loseReply     bool
	conflictReply bool
	hide          bool
	total         *string
}

func fixtureLoomGrant(subject string, roles ...string) loomUserGrant {
	g := loomUserGrant{ID: "grant-" + subject, UserID: subject, OrgID: "org", ProjectID: "loom",
		State: "USER_GRANT_STATE_ACTIVE", RoleKeys: roles}
	g.Details.Org = "org"
	return g
}

//nolint:gocognit // This bounded fixture verifies two distinct IAM wire endpoints, including unknown outcomes.
func (f *initialGrantFixture) serve(w http.ResponseWriter, r *http.Request) bool {
	if !strings.HasPrefix(r.URL.Path, "/management/v1/") {
		return false
	}
	if r.Header.Get("X-Zitadel-Orgid") != "org" || !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") ||
		r.Method != http.MethodPost {
		w.WriteHeader(http.StatusBadRequest)
		return true
	}
	w.Header().Set("Content-Type", "application/json")
	if r.URL.Path == "/management/v1/users/grants/_search" {
		f.reads++
		var body struct {
			Query struct {
				Limit int `json:"limit"`
			} `json:"query"`
			Queries []struct {
				User *struct {
					ID string `json:"userId"`
				} `json:"userIdQuery"`
				Project *struct {
					ID string `json:"projectId"`
				} `json:"projectIdQuery"`
			} `json:"queries"`
		}
		if json.NewDecoder(r.Body).Decode(&body) != nil || body.Query.Limit != identitySearchLimit ||
			len(body.Queries) != 2 || body.Queries[0].User == nil || body.Queries[1].Project == nil ||
			body.Queries[1].Project.ID != "loom" {
			w.WriteHeader(http.StatusBadRequest)
			return true
		}
		result := []loomUserGrant{}
		if g, ok := f.values[body.Queries[0].User.ID]; ok && !f.hide {
			result = append(result, g)
		}
		total := strconv.Itoa(len(result))
		if f.total != nil {
			total = *f.total
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"result": result, "details": map[string]any{"totalResult": total}})
		return true
	}
	if !strings.HasPrefix(r.URL.Path, "/management/v1/users/") || !strings.HasSuffix(r.URL.Path, "/grants") {
		w.WriteHeader(http.StatusNotFound)
		return true
	}
	var body struct {
		ProjectID string   `json:"projectId"`
		Roles     []string `json:"roleKeys"`
	}
	if json.NewDecoder(r.Body).Decode(&body) != nil || body.ProjectID != "loom" || len(body.Roles) != 1 ||
		body.Roles[0] != initialLoomRole {
		w.WriteHeader(http.StatusBadRequest)
		return true
	}
	f.writes++
	if f.status != 0 {
		w.WriteHeader(f.status)
		return true
	}
	subject := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/management/v1/users/"), "/grants")
	if _, exists := f.values[subject]; exists {
		w.WriteHeader(http.StatusConflict)
		return true
	}
	if f.values == nil {
		f.values = make(map[string]loomUserGrant)
	}
	g := fixtureLoomGrant(subject, body.Roles...)
	f.values[subject] = g
	if f.loseReply {
		conn, _, err := w.(http.Hijacker).Hijack()
		if err == nil {
			_ = conn.Close()
		}
		return true
	}
	if f.conflictReply {
		w.WriteHeader(http.StatusConflict)
		return true
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"userGrantId": g.ID})
	return true
}

func bootstrapFixtureUser(ctx context.Context, c *Client) (Account, error) {
	return c.EnsureUser(ctx, telegram.User{ID: 456, FirstName: "Fixture"}, "persisted-id",
		func(context.Context, bool) error { return nil })
}

func TestInitialLoomGrantBeforeBindingAndIdempotentRetry(t *testing.T) {
	t.Parallel()
	f, c := fixture(t)
	f.loseCreate, f.grants.loseReply = true, true
	a, err := bootstrapFixtureUser(t.Context(), c)
	require.NoError(t, err)
	require.Equal(t, "persisted-id", a.Subject)
	for range 2 {
		again, ensureErr := bootstrapFixtureUser(t.Context(), c)
		require.NoError(t, ensureErr)
		require.Equal(t, a, again)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	require.Equal(t, 1, f.creates)
	require.Equal(t, 1, f.grants.writes)
	require.Equal(t, []string{initialLoomRole}, f.grants.values[a.Subject].RoleKeys)
}

func TestInitialLoomGrantConflictRequiresExactReadBack(t *testing.T) {
	t.Parallel()
	f, c := fixture(t)
	f.grants.conflictReply = true
	_, err := bootstrapFixtureUser(t.Context(), c)
	require.NoError(t, err)
	require.Equal(t, 1, f.grants.writes)
}

func TestInitialLoomGrantFailurePreventsAccountReturn(t *testing.T) {
	t.Parallel()
	for _, status := range []int{http.StatusForbidden, http.StatusConflict, http.StatusServiceUnavailable} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			t.Parallel()
			f, c := fixture(t)
			f.grants.status = status
			account, err := bootstrapFixtureUser(t.Context(), c)
			require.Error(t, err)
			require.Empty(t, account.Subject, "caller cannot bind an account without a verified grant")
			f.mu.Lock()
			f.grants.status = 0
			f.mu.Unlock()
			account, err = bootstrapFixtureUser(t.Context(), c)
			require.NoError(t, err)
			require.Equal(t, "persisted-id", account.Subject)
			require.Equal(t, 1, f.creates, "grant retry must reuse the original IAM account")
		})
	}
}

func TestInitialLoomGrantProjectionLagDoesNotCompleteBinding(t *testing.T) {
	t.Parallel()
	f, c := fixture(t)
	f.grants.hide = true
	a, err := bootstrapFixtureUser(t.Context(), c)
	require.ErrorIs(t, err, ErrUnavailable)
	require.Empty(t, a.Subject)
	f.mu.Lock()
	f.grants.hide = false
	f.mu.Unlock()
	_, err = bootstrapFixtureUser(t.Context(), c)
	require.NoError(t, err)
	require.Equal(t, 1, f.grants.writes)
}

func TestInitialLoomGrantPreservesOtherRolesAndFailsClosedForRepair(t *testing.T) {
	t.Parallel()
	for _, roles := range [][]string{{initialLoomRole, "operator-role"}, {"operator-role"}} {
		t.Run(strings.Join(roles, "/"), func(t *testing.T) {
			t.Parallel()
			f, c := fixture(t)
			f.users = []iamUser{testUser("existing")}
			f.grants.values = map[string]loomUserGrant{"existing": fixtureLoomGrant("existing", roles...)}
			a, err := bootstrapFixtureUser(t.Context(), c)
			if len(roles) == 1 {
				require.ErrorIs(t, err, ErrInitialGrantRepair)
				require.Empty(t, a.Subject)
			} else {
				require.NoError(t, err)
				require.Equal(t, "existing", a.Subject)
			}
			require.Zero(t, f.grants.writes)
			require.Equal(t, roles, f.grants.values["existing"].RoleKeys)
		})
	}
}

func TestInitialLoomGrantRejectsForeignOrIncompleteReceipts(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{
		"user", "project", "organization", "resource owner", "project grant", "inactive", "partial", "malformed total",
	} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			f, c := fixture(t)
			f.users = []iamUser{testUser("existing")}
			g := fixtureLoomGrant("existing", initialLoomRole)
			switch kind {
			case "user":
				g.UserID = "another"
			case "project":
				g.ProjectID = "api"
			case "organization":
				g.OrgID = "foreign"
			case "resource owner":
				g.Details.Org = "foreign"
			case "project grant":
				g.ProjectGrantID = "foreign-grant"
			case "inactive":
				g.State = "USER_GRANT_STATE_INACTIVE"
			case "partial":
				total := "2"
				f.grants.total = &total
			case "malformed total":
				total := "bad"
				f.grants.total = &total
			}
			f.grants.values = map[string]loomUserGrant{"existing": g}
			a, err := bootstrapFixtureUser(t.Context(), c)
			require.Error(t, err)
			require.Empty(t, a.Subject)
			require.Zero(t, f.grants.writes)
		})
	}
}

func TestInitialLoomGrantConcurrentBootstrapCreatesOnce(t *testing.T) {
	t.Parallel()
	f, c := fixture(t)
	f.users = []iamUser{testUser("persisted-id")}
	var workers sync.WaitGroup
	for range 6 {
		workers.Go(func() {
			_, err := bootstrapFixtureUser(t.Context(), c)
			if err != nil {
				t.Error(err)
			}
		})
	}
	workers.Wait()
	f.mu.Lock()
	defer f.mu.Unlock()
	require.Len(t, f.grants.values, 1)
	require.Equal(t, []string{initialLoomRole}, f.grants.values["persisted-id"].RoleKeys)
}

func TestInitialLoomGrantRefreshesClaimsButNeverRestoresOnSessionRefresh(t *testing.T) {
	t.Parallel()
	f, c := fixture(t)
	old, err := c.UserToken(t.Context(), "persisted-id")
	require.NoError(t, err)
	f.users = []iamUser{testUser("persisted-id")}
	_, err = bootstrapFixtureUser(t.Context(), c)
	require.NoError(t, err)
	c.mu.Lock()
	_, cached := c.tokens["persisted-id"]
	c.mu.Unlock()
	require.False(t, cached)
	_, err = c.UserToken(t.Context(), "persisted-id")
	require.NoError(t, err)
	f.mu.Lock()
	before := f.grants.reads
	delete(f.grants.values, "persisted-id")
	f.mu.Unlock()
	c.Invalidate("persisted-id")
	_, err = c.UserToken(t.Context(), "persisted-id")
	require.NoError(t, err)
	f.mu.Lock()
	defer f.mu.Unlock()
	require.Equal(t, before, f.grants.reads)
	require.Empty(t, f.grants.values, "session refresh must not restore a deliberately removed role")
	require.Equal(t, 1, f.grants.writes)
	require.True(t, old.ExpiresAt.After(time.Now()))
}
