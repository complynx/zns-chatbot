package integration_test

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// identity holds the peer mutex. Model only the fixed initial role contract,
// never a generic role-management API or a permissive unknown-route fallback.
func (p *peers) initialUserGrants(w http.ResponseWriter, r *http.Request) bool {
	if !strings.HasPrefix(r.URL.Path, "/management/v1/") {
		return false
	}
	if r.Method != http.MethodPost || r.Header.Get("X-Zitadel-Orgid") != "org" ||
		!strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
		w.WriteHeader(http.StatusBadRequest)
		return true
	}
	if r.URL.Path == "/management/v1/users/grants/_search" {
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
		if json.NewDecoder(r.Body).Decode(&body) != nil || body.Query.Limit != 2 || len(body.Queries) != 2 ||
			body.Queries[0].User == nil || body.Queries[1].Project == nil || body.Queries[1].Project.ID != "loom" {
			w.WriteHeader(http.StatusBadRequest)
			return true
		}
		grants := []map[string]any{}
		if grant, ok := p.userGrants[body.Queries[0].User.ID]; ok {
			grants = append(grants, grant)
		}
		respond(w, map[string]any{"result": grants, "details": map[string]string{"totalResult": strconv.Itoa(len(grants))}})
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
		body.Roles[0] != "zinusya-user" {
		w.WriteHeader(http.StatusBadRequest)
		return true
	}
	p.grantWrites++
	if p.grantWriteStatus != 0 {
		w.WriteHeader(p.grantWriteStatus)
		return true
	}
	subject := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/management/v1/users/"), "/grants")
	if _, exists := p.users[subject]; !exists {
		w.WriteHeader(http.StatusNotFound)
		return true
	}
	if _, exists := p.userGrants[subject]; exists {
		w.WriteHeader(http.StatusConflict)
		return true
	}
	if p.userGrants == nil {
		p.userGrants = make(map[string]map[string]any)
	}
	p.userGrants[subject] = map[string]any{"id": "grant-" + subject, "userId": subject, "projectId": "loom",
		"orgId": "org", "details": map[string]string{"resourceOwner": "org"},
		"state": "USER_GRANT_STATE_ACTIVE", "roleKeys": body.Roles}
	respond(w, map[string]string{"userGrantId": "grant-" + subject})
	return true
}

func requireInitialGrant(t *testing.T, p *peers, subject string) {
	t.Helper()
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Equal(t, []string{"zinusya-user"}, p.userGrants[subject]["roleKeys"])
	require.Len(t, p.userGrants, 1)
	require.Equal(t, 1, p.grantWrites)
}

func TestInitialUserGrantTaskBootstrap(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	h := setup(t, p)
	id := h.submit(t, 1)
	h.await(t, id, "completed")
	task, err := h.store.GetTask(t.Context(), id)
	require.NoError(t, err)
	owner, err := h.store.GetUser(t.Context(), task.UserID)
	require.NoError(t, err)
	requireInitialGrant(t, p, owner.Subject)
}

func TestInitialUserGrantMiniAppBootstrap(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.miniAppOrigin = appTestOrigin
	h := setup(t, p)
	code, raw, _ := appCall(t, h, http.MethodPost, appBootstrapPath, appBody(appProof(456), 1), "", appTestOrigin)
	require.Equal(t, http.StatusOK, code, string(raw))
	var session appSession
	require.NoError(t, json.Unmarshal(raw, &session))
	owner, err := h.store.GetUser(t.Context(), session.UserID)
	require.NoError(t, err)
	requireInitialGrant(t, p, owner.Subject)
	// Existing linked sessions must respect operator removal, not restore the default.
	p.mu.Lock()
	delete(p.userGrants, owner.Subject)
	writes := p.grantWrites
	p.mu.Unlock()
	code, raw, _ = appCall(t, h, http.MethodGet, "/app/me", "", session.Token, "")
	require.Equal(t, http.StatusOK, code, string(raw))
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Empty(t, p.userGrants)
	require.Equal(t, writes, p.grantWrites)
}

func TestInitialUserGrantPrelinkBootstrap(t *testing.T) {
	t.Parallel()
	f := newPrelinkFixture(t)
	receipt := f.accepted(t, f.proof(t, 456, "initial-grant", nil))
	requireInitialGrant(t, f.p, receipt["subject"].(string))
}

func TestInitialUserGrantFailurePreventsBinding(t *testing.T) {
	t.Parallel()
	for _, path := range []string{"task", "miniapp", "prelink"} {
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			var h *harness
			var p *peers
			if path == "prelink" {
				f := newPrelinkFixture(t)
				h, p = f.h, f.p
				p.mu.Lock()
				p.grantWriteStatus = http.StatusForbidden
				p.mu.Unlock()
				code, _, err := f.call(t.Context(), f.proof(t, 456, "denied-grant", nil))
				require.NoError(t, err)
				require.GreaterOrEqual(t, code, 400)
			} else {
				p = newPeers(t)
				p.grantWriteStatus = http.StatusForbidden
				p.miniAppOrigin = appTestOrigin
				h = setup(t, p)
				if path == "miniapp" {
					code, _, _ := appCall(t, h, http.MethodPost, appBootstrapPath, appBody(appProof(456), 2), "", appTestOrigin)
					require.GreaterOrEqual(t, code, 400)
				} else {
					h.submit(t, 1)
					require.Eventually(t, func() bool { p.mu.Lock(); defer p.mu.Unlock(); return p.grantWrites > 0 },
						10*time.Second, 10*time.Millisecond)
				}
			}
			var bound int
			require.NoError(
				t,
				h.store.Pool().QueryRow(t.Context(), `SELECT count(*) FROM shuttle_users WHERE zitadel_subject<>''`).Scan(&bound),
			)
			require.Zero(t, bound)
			p.mu.Lock()
			defer p.mu.Unlock()
			require.Empty(t, p.userGrants)
			require.Empty(t, p.runs)
		})
	}
}
