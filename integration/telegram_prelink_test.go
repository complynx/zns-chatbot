package integration_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/api"
	"github.com/complynx/zns-chatbot/internal/db"
)

const prelinkPath = "/internal/identity/telegram-prelink"

type prelinkLink struct {
	Provider string `json:"idpId"`
	Subject  string `json:"userId"`
	Name     string `json:"userName"`
}

type prelinkFixture struct {
	intake           api.Acceptor
	h                *harness
	p                *peers
	provider, server *httptest.Server
	signer           jose.Signer
	key              string
	mu               sync.Mutex
	links            map[string][]prelinkLink
	effects, posts   int
	loseLinkReply    bool
	onWrite          func(string)
	loseResponse     bool
}

func newPrelinkFixture(t *testing.T) *prelinkFixture {
	t.Helper()
	p := newPeers(t)
	p.miniAppOrigin = appTestOrigin
	f := &prelinkFixture{p: p, links: make(map[string][]prelinkLink),
		key: base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{17}, 32))}
	original := p.iam.Config.Handler
	p.iam.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/v2/users/") && strings.Contains(r.URL.Path, "/links") {
			f.serveLinks(w, r)
			return
		}
		original.ServeHTTP(w, r)
	})
	private, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	f.signer, err = jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: private},
		(&jose.SignerOptions{}).WithHeader(jose.HeaderKey("kid"), "prelink-fixture"))
	require.NoError(t, err)
	f.provider = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		respond(
			w,
			jose.JSONWebKeySet{
				Keys: []jose.JSONWebKey{
					{Key: &private.PublicKey, KeyID: "prelink-fixture", Algorithm: "RS256", Use: "sig"},
				},
			},
		)
	}))
	t.Cleanup(f.provider.Close)
	f.h = setup(t, p)
	f.intake = f.h.service
	f.start(t)
	t.Cleanup(func() {
		if f.server != nil {
			f.server.Close()
		}
	})
	return f
}

func (f *prelinkFixture) start(t *testing.T) {
	t.Helper()
	endpoint, err := api.New(f.h.store, f.h.iam, f.intake, api.Options{
		BotID: 123, OrganizationID: "org", ActorID: "actor", Issuer: f.p.iam.URL,
		LoomAudience: "loom", APIAudience: "api", WebhookSecret: "test-secret",
		TelegramPrelink: api.TelegramPrelinkOptions{
			Issuer: f.provider.URL, JWKSURL: f.provider.URL + "/keys", Audience: "zinusya-prelink",
			ProviderID: "fixture-idp", ServiceBearer: f.key, HTTP: f.provider.Client(),
		},
	})
	require.NoError(t, err)
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		endpoint.InternalHandler().ServeHTTP(&prelinkReplyWriter{ResponseWriter: w, fixture: f}, r)
	}))
}

// Preserve real socket deadlines while simulating loss after committed completion.
type prelinkReplyWriter struct {
	http.ResponseWriter

	fixture *prelinkFixture
	dropped bool
}

func (w *prelinkReplyWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w *prelinkReplyWriter) WriteHeader(status int) {
	w.fixture.mu.Lock()
	drop := status == http.StatusOK && w.fixture.loseResponse
	if drop {
		w.fixture.loseResponse = false
	}
	w.fixture.mu.Unlock()
	if drop {
		w.dropped = true
		disconnect(w.ResponseWriter)
		return
	}
	w.ResponseWriter.WriteHeader(status)
}
func (w *prelinkReplyWriter) Write(body []byte) (int, error) {
	if w.dropped {
		return len(body), nil
	}
	return w.ResponseWriter.Write(body)
}

func (f *prelinkFixture) serveLinks(w http.ResponseWriter, r *http.Request) {
	f.p.mu.Lock()
	down := f.p.iamDown
	f.p.mu.Unlock()
	if down {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	user, _, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/v2/users/"), "/links")
	f.mu.Lock()
	if strings.HasSuffix(r.URL.Path, "/_search") {
		rows := append([]prelinkLink{}, f.links[user]...)
		f.mu.Unlock()
		respond(w, map[string]any{"result": rows, "details": map[string]string{"totalResult": strconv.Itoa(len(rows))}})
		return
	}
	var request struct {
		Link prelinkLink `json:"idpLink"`
	}
	if json.NewDecoder(r.Body).Decode(&request) != nil || request.Link.Provider == "" || request.Link.Subject == "" {
		f.mu.Unlock()
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	link := request.Link
	f.posts++
	for other, rows := range f.links {
		for _, row := range rows {
			if row.Provider == link.Provider && (other == user || row.Subject == link.Subject) {
				f.mu.Unlock()
				w.WriteHeader(http.StatusConflict)
				return
			}
		}
	}
	f.links[user] = append(f.links[user], link)
	f.effects++
	lose, hook := f.loseLinkReply, f.onWrite
	f.loseLinkReply = false
	f.mu.Unlock()
	if hook != nil {
		hook(user)
	}
	if lose {
		disconnect(w)
		return
	}
	respond(w, map[string]any{"details": map[string]string{"resourceOwner": "org"}})
}

func (f *prelinkFixture) proof(t *testing.T, user int64, jti string, change func(map[string]any)) string {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Second)
	numeric := strconv.FormatInt(user, 10)
	claims := map[string]any{
		"iss": f.provider.URL, "aud": "zinusya-prelink", "jti": jti, "iat": now.Unix(),
		"nbf": now.Add(-5 * time.Second).Unix(), "exp": now.Add(time.Minute).Unix(),
		"sub": "telegram:123:opaque-" + numeric, "urn:zinusya:purpose": "telegram-prelink",
		"urn:zitadeltg:telegram:bot_id": "123", "urn:zitadeltg:telegram:user_id": numeric,
		"urn:zitadeltg:telegram:subject": "opaque-" + numeric, "email": "tg+123+" + numeric + "@telegram.invalid",
		"email_verified": true, "given_name": "Provider fixture",
	}
	if change != nil {
		change(claims)
	}
	body, err := json.Marshal(claims)
	require.NoError(t, err)
	signed, err := f.signer.Sign(body)
	require.NoError(t, err)
	raw, err := signed.CompactSerialize()
	require.NoError(t, err)
	return raw
}

func (f *prelinkFixture) call(ctx context.Context, proof string) (int, []byte, error) {
	body, _ := json.Marshal(map[string]string{"assertion": proof})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, f.server.URL+prelinkPath, bytes.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+f.key)
	req.Header.Set("Content-Type", "application/json")
	response, err := f.server.Client().Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(response.Body)
	return response.StatusCode, raw, err
}

func (f *prelinkFixture) accepted(t *testing.T, proof string) map[string]any {
	t.Helper()
	code, raw, err := f.call(t.Context(), proof)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, code, string(raw))
	var receipt map[string]any
	require.NoError(t, json.Unmarshal(raw, &receipt))
	require.Equal(t, "completed", receipt["state"])
	digest := sha256.Sum256([]byte(proof))
	require.Equal(t, hex.EncodeToString(digest[:]), receipt["assertion_sha256"])
	require.NotContains(t, string(raw), f.key)
	require.NotContains(t, string(raw), proof)
	return receipt
}

func TestTelegramPrelinkConvergesBotProviderAndMiniApp(t *testing.T) {
	t.Parallel()
	for _, order := range []string{"provider-first", "bot-first", "miniapp-first"} {
		t.Run(order, func(t *testing.T) {
			t.Parallel()
			f := newPrelinkFixture(t)
			var taskID uuid.UUID
			var session appSession
			bootstrap := func() {
				code, raw, _ := appCall(
					t,
					f.h,
					http.MethodPost,
					appBootstrapPath,
					appBody(appProof(456), 19),
					"",
					appTestOrigin,
				)
				require.Equal(t, 200, code, string(raw))
				require.NoError(t, json.Unmarshal(raw, &session))
			}
			if order == "bot-first" {
				taskID = f.h.submit(t, 101)
				f.h.await(t, taskID, "completed")
			}
			if order == "miniapp-first" {
				bootstrap()
			}
			proof := f.proof(t, 456, "same-intent", nil)
			receipt := f.accepted(t, proof)
			require.Equal(t, receipt, f.accepted(t, proof))
			if order != "miniapp-first" {
				bootstrap()
			}
			if taskID == uuid.Nil {
				taskID = f.h.submit(t, 101)
				f.h.await(t, taskID, "completed")
			}
			task, err := f.h.store.GetTask(t.Context(), taskID)
			require.NoError(t, err)
			require.Equal(t, session.UserID, task.UserID)
			require.Equal(t, session.UserID.String(), receipt["user_id"])
			f.p.mu.Lock()
			require.Len(t, f.p.users, 1)
			f.p.mu.Unlock()
			f.mu.Lock()
			require.Equal(t, 1, f.effects)
			require.Equal(t, "telegram:123:opaque-456", f.links[receipt["subject"].(string)][0].Subject)
			f.mu.Unlock()
		})
	}
}

func TestTelegramPrelinkLostLinkAndCompletionReplyRecoverSameReceipt(t *testing.T) {
	t.Parallel()
	f := newPrelinkFixture(t)
	f.mu.Lock()
	f.loseLinkReply, f.loseResponse = true, true
	f.mu.Unlock()
	proof := f.proof(t, 456, "lost-response", nil)
	_, _, err := f.call(t.Context(), proof)
	require.Error(t, err, "Completion is committed but the HTTP reply is lost")
	f.server.Close()
	f.start(t)
	receipt := f.accepted(t, proof)
	require.Equal(t, receipt, f.accepted(t, proof))
	var count int
	require.NoError(
		t,
		f.h.store.Pool().
			QueryRow(t.Context(), "SELECT count(*) FROM shuttle_telegram_prelinks WHERE state='completed'").
			Scan(&count),
	)
	require.Equal(t, 1, count)
	f.mu.Lock()
	require.Equal(t, 1, f.effects)
	require.Equal(t, 1, f.posts)
	f.mu.Unlock()
}

func TestTelegramPrelinkIAMOutageKeepsIntentAndResumes(t *testing.T) {
	t.Parallel()
	f := newPrelinkFixture(t)
	f.p.mu.Lock()
	f.p.iamDown = true
	f.p.mu.Unlock()
	proof := f.proof(t, 456, "iam-outage", nil)
	code, _, err := f.call(t.Context(), proof)
	require.NoError(t, err)
	require.Equal(t, 503, code)
	var pending string
	require.NoError(
		t,
		f.h.store.Pool().
			QueryRow(t.Context(), "SELECT id::text FROM shuttle_telegram_prelinks WHERE state='pending'").
			Scan(&pending),
	)
	f.p.mu.Lock()
	f.p.iamDown = false
	f.p.mu.Unlock()
	f.server.Close()
	f.start(t)
	receipt := f.accepted(t, proof)
	require.Equal(t, pending, receipt["operation_id"])
	var count int
	require.NoError(t, f.h.store.Pool().QueryRow(t.Context(), "SELECT count(*) FROM shuttle_tasks").Scan(&count))
	require.Zero(t, count, "Provider intake does not launch agent work")
}

func TestTelegramPrelinkRevocationPreventsCompletionAndReplay(t *testing.T) {
	t.Parallel()
	for _, phase := range []string{"pending", "completed"} {
		t.Run(phase, func(t *testing.T) {
			t.Parallel()
			f := newPrelinkFixture(t)
			revoke := func() {
				_, err := f.h.store.Pool().
					Exec(t.Context(), "UPDATE shuttle_users SET revoked=true WHERE bot_id=123 AND tg_user_id=456")
				if err != nil {
					t.Errorf("revoke fixture: %v", err)
				}
			}
			proof := f.proof(t, 456, "revoked", nil)
			if phase == "completed" {
				f.accepted(t, proof)
				revoke()
			} else {
				f.mu.Lock()
				f.onWrite = func(string) { revoke() }
				f.mu.Unlock()
			}
			code, _, err := f.call(t.Context(), proof)
			require.NoError(t, err)
			require.Equal(t, 403, code)
			var state string
			require.NoError(
				t,
				f.h.store.Pool().QueryRow(t.Context(), "SELECT state FROM shuttle_telegram_prelinks").Scan(&state),
			)
			require.Equal(t, phase, state)
		})
	}
}

func TestTelegramPrelinkRejectsProofBeforeLocalOrIAMEffects(t *testing.T) {
	t.Parallel()
	f := newPrelinkFixture(t)
	for _, field := range []string{"iss", "aud", "urn:zinusya:purpose", "urn:zitadeltg:telegram:bot_id", "email"} {
		proof := f.proof(t, 456, "invalid-"+field, func(claims map[string]any) { claims[field] = "wrong" })
		code, _, err := f.call(t.Context(), proof)
		require.NoError(t, err)
		require.Equal(t, 403, code)
	}
	var count int
	require.NoError(t, f.h.store.Pool().QueryRow(t.Context(), "SELECT count(*) FROM shuttle_users").Scan(&count))
	require.Zero(t, count)
	f.p.mu.Lock()
	require.Empty(t, f.p.users)
	f.p.mu.Unlock()
}

func TestTelegramPrelinkChangedDigestAndForeignOpaqueBindingConflict(t *testing.T) {
	t.Parallel()
	f := newPrelinkFixture(t)
	proof := f.proof(t, 456, "immutable", nil)
	original := f.accepted(t, proof)
	changed := f.proof(t, 456, "immutable", func(claims map[string]any) { claims["given_name"] = "Changed" })
	code, _, err := f.call(t.Context(), changed)
	require.NoError(t, err)
	require.Equal(t, 409, code)
	foreign := f.proof(t, 789, "foreign", func(claims map[string]any) {
		claims["sub"] = "telegram:123:opaque-456"
		claims["urn:zitadeltg:telegram:subject"] = "opaque-456"
	})
	code, _, err = f.call(t.Context(), foreign)
	require.NoError(t, err)
	require.Equal(t, 409, code)
	require.Equal(t, original, f.accepted(t, proof))
	f.mu.Lock()
	require.Equal(t, 1, f.effects)
	f.mu.Unlock()
}

func TestTelegramPrelinkConcurrentFirstLoginConverges(t *testing.T) {
	t.Parallel()
	f := newPrelinkFixture(t)
	proof := f.proof(t, 456, "concurrent", nil)
	type result struct {
		code int
		raw  []byte
		err  error
	}
	replies := make(chan result, 8)
	start := make(chan struct{})
	for range 8 {
		go func() { <-start; code, raw, err := f.call(t.Context(), proof); replies <- result{code, raw, err} }()
	}
	appReply := make(chan result, 1)
	go func() {
		<-start
		code, raw, _, err := appHTTPCall(
			f.h,
			http.MethodPost,
			appBootstrapPath,
			appBody(appProof(456), 23),
			"",
			appTestOrigin,
		)
		appReply <- result{code, raw, err}
	}()
	close(start)
	taskID := f.h.submit(t, 401)
	first := <-replies
	require.NoError(t, first.err)
	require.Equal(t, 200, first.code, string(first.raw))
	for range 7 {
		reply := <-replies
		require.NoError(t, reply.err)
		require.Equal(t, 200, reply.code, string(reply.raw))
		require.JSONEq(t, string(first.raw), string(reply.raw))
	}
	app := <-appReply
	require.NoError(t, app.err)
	require.Equal(t, 200, app.code, string(app.raw))
	var session appSession
	require.NoError(t, json.Unmarshal(app.raw, &session))
	f.h.await(t, taskID, "completed")
	task, err := f.h.store.GetTask(t.Context(), taskID)
	require.NoError(t, err)
	require.Equal(t, session.UserID, task.UserID)
	f.mu.Lock()
	require.Equal(t, 1, f.effects)
	f.mu.Unlock()
}

func TestTelegramPrelinkReleasesOnlyDBConnectionDuringIAM(t *testing.T) {
	t.Parallel()
	f := newPrelinkFixture(t)
	f.server.Close()
	f.h.stop(t)
	dsn, err := url.Parse(f.h.store.Pool().Config().ConnString())
	require.NoError(t, err)
	query := dsn.Query()
	query.Set("pool_max_conns", "1")
	dsn.RawQuery = query.Encode()
	f.h.store.Close()
	reopened, err := db.Open(t.Context(), dsn.String())
	require.NoError(t, err)
	f.h.store = reopened
	f.start(t)
	t.Cleanup(func() { f.server.Close(); f.server = nil; reopened.Close() })
	require.Equal(t, int32(1), reopened.Pool().Config().MaxConns)
	entered, release := make(chan struct{}, 1), make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	f.mu.Lock()
	f.onWrite = func(string) { entered <- struct{}{}; <-release }
	f.mu.Unlock()
	type completion struct {
		code int
		err  error
	}
	result := make(chan completion, 1)
	proof := f.proof(t, 456, "single-connection", nil)
	go func() { code, _, callErr := f.call(t.Context(), proof); result <- completion{code, callErr} }()
	select {
	case <-entered:
	case <-t.Context().Done():
		t.Fatal(t.Context().Err())
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	connection, err := reopened.Pool().Acquire(ctx)
	require.NoError(t, err, "IAM wait must not retain the only database connection")
	connection.Release()
	releaseOnce.Do(func() { close(release) })
	reply := <-result
	require.NoError(t, reply.err)
	require.Equal(t, 200, reply.code)
}
