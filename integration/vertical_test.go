package integration_test

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/api"
	"github.com/complynx/zns-chatbot/internal/avatar"
	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/identity"
	"github.com/complynx/zns-chatbot/internal/loom"
	"github.com/complynx/zns-chatbot/internal/observability"
	"github.com/complynx/zns-chatbot/internal/registrationdetails"
	"github.com/complynx/zns-chatbot/internal/task"
	"github.com/complynx/zns-chatbot/internal/telegram"
)

// These tests exercise real durable acceptance and recovery; upstream services
// remain HTTP peers so their uncertainty is observed through public behavior.
type peers struct {
	registrationDetails                     *registrationdetails.Protector
	retainResolvedPrompts                   bool
	miniAppOrigin                           string
	miniAppLaunchURL                        string
	miniAppAllowLocalHTTP                   bool
	loseUserCreateAndStopIAM                bool
	avatarProcessor                         avatar.Processor
	documentSent                            []telegram.SendDocumentRequest
	loseDocumentSend                        bool
	documentAttempts                        []time.Time
	documentRejectOnce                      int
	documentWrongReceipt                    bool
	documentEntered                         chan struct{}
	documentRelease                         chan struct{}
	fileBytes                               []byte
	fileReadHook                            func()
	fileGone                                bool
	fileTooLarge                            bool
	fileGets                                int
	fileIDs                                 []string
	loseResume                              bool
	resumeKeys                              []string
	resumeEffects                           int
	resumeBlocked                           bool
	dropResume                              bool
	runGets                                 int
	resumeReceipts                          map[string]loom.ControlReceipt
	loomAuthDenied                          bool
	loomAuthDeniedCalls                     int
	sessionReauthorizations                 int
	reauthorizationVersions                 []int64
	keepWaitingAuth                         bool
	loseReauthorization                     bool
	adminRole                               bool
	loseAuth                                bool
	getEntered                              chan struct{}
	getRelease                              chan struct{}
	echoInput                               bool
	createdInputs                           []string
	createAttempts                          []string
	pauseAfterLostCreate                    bool
	steering                                bool
	result                                  string
	firstInputEntered                       chan struct{}
	firstInputRelease                       chan struct{}
	firstInputCanceled                      chan struct{}
	firstInputOnce                          sync.Once
	firstInputCancelOnce                    sync.Once
	inputKeys                               []string
	inputAttempts                           []loom.InputRequest
	inputReceipts                           map[string]loom.ControlReceipt
	inputBodies                             map[string]string
	loseInputAndStopLoom                    bool
	terminalInputConflict                   bool
	secondInputEntered                      chan struct{}
	sendEntered                             chan struct{}
	sendRelease                             chan struct{}
	cancelKeys                              []string
	loomUnavailableCalls                    int
	mu                                      sync.Mutex
	iam, loom, tg                           *httptest.Server
	key                                     *ecdsa.PrivateKey
	signer                                  jose.Signer
	users                                   map[string]map[string]any
	unavailableTokenSubjects                map[string]bool
	tokenHook                               func(string)
	tokenExpiry                             time.Time
	runs                                    map[string]loom.Run
	createKeys                              []string
	sent                                    []telegram.SendMessageRequest
	iamDown, loomDown, loseCreate, loseSend bool
	inputResult                             *string
	questionChoices                         []loom.Choice
	question                                bool
	approval                                *loom.Approval
	inputs                                  []loom.InputRequest
	callbackAcks                            []string
	loseAck                                 bool
	rejectSendOnce                          bool
	userInactive                            bool
	rejectInputAndFinish                    bool
	unknownInputBeforeRejection             bool
	rejectInputStatus                       int
	getMeUser                               *telegram.User
	getMeFailure                            bool
	getMeEntered, getMeRelease              chan struct{}
}

func newPeers(t *testing.T) *peers {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.ES256, Key: key},
		(&jose.SignerOptions{}).WithHeader("kid", "fixture"),
	)
	require.NoError(t, err)
	p := &peers{key: key, signer: signer, users: map[string]map[string]any{}, runs: map[string]loom.Run{}}
	p.iam = httptest.NewServer(http.HandlerFunc(p.identity))
	p.loom = httptest.NewServer(http.HandlerFunc(p.agent))
	p.tg = httptest.NewServer(http.HandlerFunc(p.telegram))
	t.Cleanup(p.iam.Close)
	t.Cleanup(p.loom.Close)
	t.Cleanup(p.tg.Close)
	return p
}
func respond(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
func disconnect(w http.ResponseWriter) {
	if h, ok := w.(http.Hijacker); ok {
		c, _, e := h.Hijack()
		if e == nil {
			_ = c.Close()
		}
	}
}
func (p *peers) credentialExpiry() time.Time {
	if !p.tokenExpiry.IsZero() {
		return p.tokenExpiry
	}
	return time.Now().Add(time.Hour)
}

func (p *peers) token(subject string) string {
	now := time.Now()
	// These scenarios do not test issuance time; allow small VM clock steps.
	issuedAt := now.Add(-time.Minute)
	claims := map[string]any{
		"iss":                                   p.iam.URL,
		"sub":                                   subject,
		"aud":                                   []string{"loom", "api"},
		"exp":                                   p.credentialExpiry().Unix(),
		"iat":                                   issuedAt.Unix(),
		"urn:zitadel:iam:user:resourceowner:id": "org",
		"urn:zitadel:iam:org:project:roles":     map[string]any{"account.me": map[string]string{"org": "Org"}},
		"urn:zitadel:iam:org:project:api:roles": map[string]any{"account.me": map[string]string{"org": "Org"}},
	}
	if p.adminRole {
		claims["urn:zitadel:iam:org:project:api:roles"] = map[string]any{
			"account.me":    map[string]string{"org": "Org"},
			"zinusya.admin": map[string]string{"org": "Org"},
		}
	}
	if subject != "actor" {
		claims["act"] = map[string]string{"sub": "actor"}
	}
	result, e := jwt.Signed(p.signer).Claims(claims).Serialize()
	if e != nil {
		panic(e)
	}
	return result
}
func (p *peers) identity(w http.ResponseWriter, r *http.Request) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.iamDown {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	switch r.URL.Path {
	case "/.well-known/openid-configuration":
		respond(
			w,
			map[string]any{
				"issuer":                 p.iam.URL,
				"jwks_uri":               p.iam.URL + "/keys",
				"token_endpoint":         p.iam.URL + "/token",
				"introspection_endpoint": p.iam.URL + "/intro",
			},
		)
	case "/keys":
		respond(
			w,
			jose.JSONWebKeySet{
				Keys: []jose.JSONWebKey{{Key: &p.key.PublicKey, KeyID: "fixture", Algorithm: "ES256", Use: "sig"}},
			},
		)
	case "/token":
		p.issueToken(w, r)
	case "/intro":
		p.introspect(w, r)
	case "/v2/users":
		var request struct {
			Queries []struct {
				EmailQuery struct {
					Email string `json:"emailAddress"`
				} `json:"emailQuery"`
			} `json:"queries"`
		}
		_ = json.NewDecoder(r.Body).Decode(&request)
		found := []map[string]any{}
		for _, u := range p.users {
			human := u["human"].(map[string]any)
			email := human["email"].(map[string]any)["email"]
			for _, q := range request.Queries {
				if q.EmailQuery.Email == email {
					found = append(found, u)
				}
			}
		}
		respond(w, map[string]any{"result": found, "details": map[string]string{"totalResult": "0"}})
	case "/v2/users/new":
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		id := body["userId"].(string)
		p.users[id] = map[string]any{
			"userId":  id,
			"state":   "USER_STATE_ACTIVE",
			"details": map[string]string{"resourceOwner": "org"},
			"human":   body["human"],
		}
		if p.loseUserCreateAndStopIAM {
			p.loseUserCreateAndStopIAM = false
			p.iamDown = true
			disconnect(w)
			return
		}
		respond(w, map[string]string{"id": id})
	default:
		if u, ok := p.users[strings.TrimPrefix(r.URL.Path, "/v2/users/")]; ok {
			respond(w, map[string]any{"user": u})
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}
}
func (p *peers) introspect(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	parsed, err := jwt.ParseSigned(r.Form.Get("token"), []jose.SignatureAlgorithm{jose.ES256})
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	claims := map[string]any{}
	if parsed.Claims(&p.key.PublicKey, &claims) != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	claims["active"] = !p.userInactive || claims["sub"] == "actor"
	respond(w, claims)
}

func (p *peers) agent(w http.ResponseWriter, r *http.Request) {
	if !p.waitGet(r) {
		return
	}
	if !p.delayInput(r) {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.rejectAgentRequest(w, r) {
		return
	}
	switch {
	case strings.HasSuffix(r.URL.Path, "/auth/sessions"), strings.HasSuffix(r.URL.Path, "/reauthorize"):
		p.authSession(w, r)
	case strings.HasSuffix(r.URL.Path, "/runs") && r.Method == http.MethodPost:
		p.createRun(w, r)
	case strings.HasSuffix(r.URL.Path, "/input"):
		p.receiveInput(w, r)
	case strings.HasSuffix(r.URL.Path, "/resume"):
		p.resumeRun(w, r)
	case strings.HasSuffix(r.URL.Path, "/cancel"):
		p.cancelRun(w, r)
	default:
		for _, run := range p.runs {
			if strings.HasSuffix(r.URL.Path, "/runs/"+run.ID) {
				p.runGets++
				respond(w, run)
				return
			}
		}
		w.WriteHeader(http.StatusNotFound)
	}
}
func (p *peers) receiveInput(w http.ResponseWriter, r *http.Request) {
	if p.unknownInputBeforeRejection || p.loseInputAndStopLoom {
		p.unknownInputBeforeRejection = false
		p.applyInput(httptest.NewRecorder(), r)
		if p.loseInputAndStopLoom {
			p.loseInputAndStopLoom = false
			p.loomDown = true
		}
		disconnect(w)
		return
	}
	if p.rejectInputAndFinish {
		p.rejectInputAndFinish = false
		p.steering = false
		for key, run := range p.runs {
			run.Status = "completed"
			run.Result = "first message"
			p.runs[key] = run
		}
		w.WriteHeader(p.rejectInputStatus)
		return
	}
	p.applyInput(w, r)
}

func (p *peers) resumeRun(w http.ResponseWriter, r *http.Request) {
	key := r.Header.Get("Idempotency-Key")
	p.resumeKeys = append(p.resumeKeys, key)
	if p.dropResume {
		p.dropResume = false
		disconnect(w)
		return
	}
	if p.resumeBlocked {
		w.WriteHeader(http.StatusConflict)
		return
	}
	if receipt, ok := p.resumeReceipts[key]; ok {
		respond(w, receipt)
		return
	}
	for id, run := range p.runs {
		if !strings.HasSuffix(r.URL.Path, "/runs/"+run.ID+"/resume") {
			continue
		}
		if run.Status != "paused" {
			w.WriteHeader(http.StatusConflict)
			return
		}
		run.Status, run.Reason = "running", ""
		run.Version++
		p.runs[id] = run
		p.resumeEffects++
		receipt := loom.ControlReceipt{
			ID:      run.ID,
			Status:  run.Status,
			Version: run.Version,
			InputID: "input-" + key,
			Context: &loom.InputContext{Revision: int64(len(p.inputs)), SourceActor: "fixture"},
		}
		if p.resumeReceipts == nil {
			p.resumeReceipts = map[string]loom.ControlReceipt{}
		}
		p.resumeReceipts[key] = receipt
		if p.loseResume {
			p.loseResume = false
			disconnect(w)
			return
		}
		respond(w, receipt)
		return
	}
	w.WriteHeader(http.StatusNotFound)
}
func (p *peers) cancelRun(w http.ResponseWriter, r *http.Request) {
	p.cancelKeys = append(p.cancelKeys, r.Header.Get("Idempotency-Key"))
	for key, run := range p.runs {
		if strings.HasSuffix(r.URL.Path, "/runs/"+run.ID+"/cancel") {
			run.Status = "cancelled"
			run.Question = nil
			run.Version++
			p.runs[key] = run
			respond(w, loom.ControlReceipt{ID: run.ID, Status: run.Status, Version: run.Version})
			return
		}
	}
	w.WriteHeader(http.StatusNotFound)
}
func (p *peers) createRun(w http.ResponseWriter, r *http.Request) {
	var request loom.CreateRunRequest
	_ = json.NewDecoder(r.Body).Decode(&request)
	key := r.Header.Get("Idempotency-Key")
	p.createKeys = append(p.createKeys, key)
	p.createAttempts = append(p.createAttempts, request.Input.Text)
	if r.Header.Get("X-Auth-Session-Id") != "session" || key == "" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	run, ok := p.runs[key]
	if !ok {
		p.createdInputs = append(p.createdInputs, request.Input.Text)
		run = p.initialRun(key, request)
		p.runs[key] = run
	}
	if p.loseCreate {
		p.loseCreate = false
		if p.pauseAfterLostCreate {
			p.loomDown = true
		}
		disconnect(w)
		return
	}
	respond(w, run)
}
func (p *peers) initialRun(key string, request loom.CreateRunRequest) loom.Run {
	run := loom.Run{
		ID:            "run-" + key,
		Interaction:   request.Interaction,
		AuthSessionID: "session",
		Status:        "completed",
		Result:        "**Hello**, Daniel!",
		Version:       1,
	}
	if p.question {
		run.Status = "awaiting_input"
		run.Result = ""
		run.Question = &loom.Question{
			ID:        "question-1",
			Choices:   p.questionChoices,
			Text:      "What is your name?",
			AllowText: true,
			ExpiresAt: time.Now().Add(time.Hour),
		}
	}
	if p.approval != nil {
		approval := *p.approval
		if approval.ReplyMode == "" {
			approval.ReplyMode = "agent"
		}
		run.Status, run.Result, run.Approval = "awaiting_input", "", &approval
	}
	if p.steering {
		run.Status = "running"
		run.Result = ""
	}
	if p.echoInput && !p.steering {
		run.Result = promptUserText(request.Input.Text)
	}
	if p.result != "" {
		run.Result = p.result
	}
	return run
}
func (p *peers) telegram(w http.ResponseWriter, r *http.Request) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if strings.HasSuffix(r.URL.Path, "/getMe") {
		p.telegramMe(w)
		return
	}
	if strings.HasSuffix(r.URL.Path, "/sendDocument") {
		p.sendDocument(w, r)
		return
	}
	if strings.HasSuffix(r.URL.Path, "/getFile") {
		p.fileGets++
		if p.fileGone {
			respond(w, map[string]any{"ok": false, "error_code": 400})
			return
		}
		if p.fileTooLarge {
			w.WriteHeader(http.StatusBadRequest)
			respond(w, map[string]any{"ok": false, "error_code": 400, "description": "Bad Request: file is too big"})
			return
		}
		var in struct {
			ID string `json:"file_id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		p.fileIDs = append(p.fileIDs, in.ID)
		respond(
			w,
			map[string]any{
				"ok": true,
				"result": map[string]any{
					"file_id":   in.ID,
					"file_size": len(p.fileBytes),
					"file_path": fmt.Sprintf("documents/refreshed_%d.pdf", p.fileGets),
				},
			},
		)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/file/bot123:test-token/") {
		if p.fileGone {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if p.fileReadHook != nil {
			hook := p.fileReadHook
			p.fileReadHook = nil
			hook()
		}
		_, _ = w.Write(p.fileBytes)
		return
	}
	if strings.HasSuffix(r.URL.Path, "/answerCallbackQuery") {
		var request struct {
			ID string `json:"callback_query_id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&request)
		p.callbackAcks = append(p.callbackAcks, request.ID)
		if p.loseAck {
			p.loseAck = false
			disconnect(w)
			return
		}
		respond(w, map[string]any{"ok": true, "result": true})
		return
	}
	var message telegram.SendMessageRequest
	if !strings.HasSuffix(r.URL.Path, "/sendMessage") {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	_ = json.NewDecoder(r.Body).Decode(&message)
	p.sent = append(p.sent, message)
	if p.rejectSendOnce {
		p.rejectSendOnce = false
		w.WriteHeader(http.StatusTooManyRequests)
		respond(w, map[string]any{"ok": false, "error_code": 429, "parameters": map[string]int{"retry_after": 2}})
		return
	}
	if p.sendEntered != nil {
		close(p.sendEntered)
		p.sendEntered = nil
		<-p.sendRelease
	}
	if p.loseSend {
		p.loseSend = false
		disconnect(w)
		return
	}
	respond(
		w,
		map[string]any{
			"ok":     true,
			"result": map[string]any{"message_id": len(p.sent), "chat": map[string]int64{"id": message.ChatID}},
		},
	)
}

// telegramMe runs under the peer mutex, like the other mock Telegram methods.
func (p *peers) telegramMe(w http.ResponseWriter) {
	if p.getMeEntered != nil {
		close(p.getMeEntered)
		p.getMeEntered = nil
		<-p.getMeRelease
	}
	if p.getMeFailure {
		w.WriteHeader(http.StatusServiceUnavailable)
		respond(w, map[string]any{"ok": false, "error_code": 503})
		return
	}
	bot := telegram.User{ID: 123, IsBot: true, Username: "zinusya_test_bot"}
	if p.getMeUser != nil {
		bot = *p.getMeUser
	}
	respond(w, map[string]any{"ok": true, "result": bot})
}

type harness struct {
	store    *db.Store
	peers    *peers
	service  *task.Service
	server   *httptest.Server
	endpoint *api.Server
	iam      *identity.Client
	observer *observability.Telemetry
}

func prepareHarness(t *testing.T, p *peers) *harness {
	t.Helper()
	dsn := os.Getenv("SHUTTLE_TEST_DATABASE_URL")
	if dsn == "" {
		dsn = os.Getenv("SHUTTLE_TEST_DATABASE_DSN")
	}
	if dsn == "" {
		t.Skip("SHUTTLE_TEST_DATABASE_URL required for PostgreSQL integration tests")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err)
	schema := "integration_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	_, err = pool.Exec(ctx, "CREATE SCHEMA "+schema)
	require.NoError(t, err)
	parsed, err := url.Parse(dsn)
	require.NoError(t, err)
	q := parsed.Query()
	q.Set("search_path", schema)
	q.Set("application_name", schema)
	// Queue workers and lock observers need capacity beyond blocked business transactions.
	if q.Get("pool_max_conns") == "" {
		q.Set("pool_max_conns", "8")
	}
	parsed.RawQuery = q.Encode()
	store, err := db.Open(ctx, parsed.String())
	require.NoError(t, err)
	t.Cleanup(func() {
		store.Close()
		// Dedicated browser QA exports failed fixtures before retiring its database.
		if !t.Failed() || os.Getenv("ZINUSYA_QA_KEEP_FAILED_SCHEMA") != "1" {
			_, _ = pool.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE")
		}
		pool.Close()
	})
	require.NoError(t, store.Migrate(ctx))
	require.NoError(t, task.MigrateQueue(ctx, store.Pool()))
	return &harness{store: store, peers: p}
}

func setup(t *testing.T, p *peers) *harness {
	t.Helper()
	h := prepareHarness(t, p)
	h.start(t)
	t.Cleanup(func() { h.stop(t) })
	return h
}

func (h *harness) start(t *testing.T) {
	t.Helper()
	p := h.peers
	iam, err := identity.New(
		identity.Options{
			Issuer:                         p.iam.URL,
			OrganizationID:                 "org",
			BotID:                          123,
			LoomAudience:                   "loom",
			APIAudience:                    "api",
			ActorID:                        "actor",
			ActorClientCredentials:         identity.ClientCredentials{ID: "actor-client", Secret: "secret"},
			ExchangeClientCredentials:      identity.ClientCredentials{ID: "exchange", Secret: "secret"},
			IntrospectionClientCredentials: identity.ClientCredentials{ID: "intro", Secret: "secret"},
		},
		p.iam.Client(),
	)
	require.NoError(t, err)
	h.iam = iam
	agent, err := loom.New(p.loom.URL, p.loom.Client())
	require.NoError(t, err)
	tg, err := telegram.New(p.tg.URL, "123:test-token", p.tg.Client())
	require.NoError(t, err)
	service, err := task.New(
		h.store,
		iam,
		agent,
		tg,
		h.observer,
		slog.New(slog.DiscardHandler),
		task.Options{
			BotID:                      123,
			RegistrationDetailsEnabled: p.registrationDetails != nil,
			MiniAppURL: func() string {
				if p.miniAppLaunchURL != "" {
					return p.miniAppLaunchURL
				}
				if p.miniAppOrigin != "" {
					return p.miniAppOrigin + "/app/"
				}
				return ""
			}(),
			PollInterval:   50 * time.Millisecond,
			AttemptTimeout: 2 * time.Second,
			LeaseDuration:  3 * time.Second,
			MaxWorkers:     2,
		},
	)
	require.NoError(t, err)
	h.service = service
	endpoint, err := api.New(
		h.store,
		iam,
		service,
		api.Options{
			RegistrationDetails:   p.registrationDetails,
			MiniAppOrigin:         p.miniAppOrigin,
			MiniAppAllowLocalHTTP: p.miniAppAllowLocalHTTP,
			MiniAppBotToken:       "123:test-token",
			AvatarRenderer:        p.avatarProcessor,
			Telegram:              tg,
			BotID:                 123,
			WebhookSecret:         "test-secret",
			OrganizationID:        "org",
			ActorID:               "actor",
			Issuer:                p.iam.URL,
			LoomAudience:          "loom",
		},
	)
	require.NoError(t, err)
	h.endpoint = endpoint
	h.server = httptest.NewServer(endpoint.PublicHandler())
	require.NoError(t, service.Start(context.Background()))
}
func (h *harness) stop(t *testing.T) {
	t.Helper()
	if h.server != nil {
		h.server.Close()
		h.server = nil
	}
	if h.service != nil {
		// Fixture teardown uses the hard bound; process tests cover the soft drain budget.
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		require.NoError(t, h.service.Stop(ctx))
		h.service = nil
	}
}
func (h *harness) submit(t *testing.T, id int64) uuid.UUID {
	t.Helper()
	return h.submitText(t, id, "Hello")
}
func (h *harness) submitText(t *testing.T, id int64, text string) uuid.UUID {
	t.Helper()
	body, err := json.Marshal(
		telegram.Update{
			ID: id,
			Message: &telegram.Message{
				ID:   id,
				From: &telegram.User{ID: 456, FirstName: "Daniel", LanguageCode: "en"},
				Chat: telegram.Chat{ID: 456, Type: "private"},
				Text: text,
			},
		},
	)
	require.NoError(t, err)
	req, err := http.NewRequest(http.MethodPost, h.server.URL+"/telegram/webhook", bytes.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("X-Telegram-Bot-Api-Secret-Token", "test-secret")
	response, err := h.server.Client().Do(req)
	require.NoError(t, err)
	defer response.Body.Close()
	require.Less(t, response.StatusCode, 300)
	var accepted struct {
		ID uuid.UUID `json:"task_id"`
	}
	require.NoError(t, json.NewDecoder(response.Body).Decode(&accepted))
	require.NotEqual(t, uuid.Nil, accepted.ID)
	return accepted.ID
}
func (h *harness) await(t *testing.T, id uuid.UUID, state string) {
	t.Helper()
	require.Eventually(
		t,
		func() bool { v, e := h.store.GetTask(context.Background(), id); return e == nil && v.State == state },
		15*time.Second,
		25*time.Millisecond,
	)
}
func (h *harness) awaitReplies(t *testing.T, n int) {
	t.Helper()
	require.Eventually(
		t,
		func() bool { h.peers.mu.Lock(); defer h.peers.mu.Unlock(); return len(h.peers.sent) == n },
		15*time.Second,
		25*time.Millisecond,
	)
}

func TestAcceptedMessageRunsAndDuplicateDoesNotRepeat(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	h := setup(t, p)
	id := h.submit(t, 1)
	require.Equal(t, id, h.submit(t, 1))
	h.await(t, id, "completed")
	h.awaitReplies(t, 1)
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Len(t, p.users, 1)
	require.Len(t, p.runs, 1)
	require.Equal(t, "Hello, Daniel!", p.sent[0].Text)
	require.NotEmpty(t, p.sent[0].Entities)
}
func TestLostRunCreationReceiptUsesSameOperation(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.loseCreate = true
	h := setup(t, p)
	id := h.submit(t, 2)
	h.await(t, id, "completed")
	h.awaitReplies(t, 1)
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Len(t, p.runs, 1)
	require.GreaterOrEqual(t, len(p.createKeys), 2)
	for _, key := range p.createKeys {
		require.Equal(t, id.String(), key)
	}
}
func TestAcceptedWorkSurvivesBotRestartAndBothUpstreamOutages(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.iamDown = true
	p.loomDown = true
	h := setup(t, p)
	id := h.submit(t, 3)
	h.await(t, id, "awaiting_authorization")
	p.mu.Lock()
	p.iamDown = false
	p.mu.Unlock()
	// The restored IAM alone must not lose work while Loom is still unavailable.
	require.Eventually(
		t,
		func() bool { p.mu.Lock(); defer p.mu.Unlock(); return p.loomUnavailableCalls > 0 },
		15*time.Second,
		25*time.Millisecond,
	)
	h.stop(t)
	p.mu.Lock()
	p.iamDown = false
	p.loomDown = false
	p.mu.Unlock()
	h.start(t)
	h.await(t, id, "completed")
	h.awaitReplies(t, 1)
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Len(t, p.runs, 1)
	require.Len(t, p.users, 1)
}
func TestUnknownTelegramReceiptIsNotSentAgainAfterRestart(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.loseSend = true
	h := setup(t, p)
	id := h.submit(t, 4)
	h.await(t, id, "delivery_unknown")
	h.awaitReplies(t, 1)
	h.stop(t)
	h.start(t)
	// Redelivery of the same Telegram update must not reopen an uncertain send.
	require.Equal(t, id, h.submit(t, 4))
	h.await(t, id, "delivery_unknown")
	h.stop(t)
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Len(t, p.sent, 1)
}

func TestRevocationBlocksDeferredOutputAndPreservesDenialAfterRestart(t *testing.T) {
	t.Parallel()
	for _, question := range []bool{false, true} {
		name := "result"
		if question {
			name = "question"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			p := newPeers(t)
			p.question = question
			p.rejectSendOnce = true
			h := setup(t, p)
			id := h.submit(t, 62)
			require.Eventually(t, func() bool {
				rows, err := h.store.ListOutbox(context.Background(), id)
				return err == nil && len(rows) == 1 && rows[0].State == "retryable"
			}, 10*time.Second, 20*time.Millisecond)
			accepted, err := h.store.GetTask(context.Background(), id)
			require.NoError(t, err)
			_, err = h.store.SetUserRevoked(context.Background(), accepted.UserID, "admin")
			require.NoError(t, err)
			h.stop(t)
			h.start(t)
			state := "delivery_failed"
			if question {
				state = "failed"
			}
			h.await(t, id, state)
			if question {
				require.Eventually(t, func() bool {
					rows, checkErr := h.store.ListOutbox(context.Background(), id)
					return checkErr == nil && len(rows) == 2 && rows[1].State == "delivered"
				}, 10*time.Second, 20*time.Millisecond)
			}
			h.stop(t)
			rows, err := h.store.ListOutbox(context.Background(), id)
			require.NoError(t, err)
			require.Equal(t, "delivery_blocked", rows[0].State)
			p.mu.Lock()
			defer p.mu.Unlock()
			if question {
				require.Len(t, p.sent, 2)
				require.Equal(t, "Access denied. Please contact the administrator.", p.sent[1].Text)
				require.Equal(t, "delivered", rows[1].State)
			} else {
				require.Len(t, p.sent, 1)
			}
		})
	}
}

func TestDeferredResultRechecksIAMAndWaitsForTemporaryOutage(t *testing.T) {
	t.Parallel()
	for _, outage := range []bool{false, true} {
		name := "explicit_denial"
		if outage {
			name = "temporary_outage"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			p := newPeers(t)
			p.rejectSendOnce = true
			h := setup(t, p)
			id := h.submit(t, 63)
			require.Eventually(t, func() bool {
				rows, checkErr := h.store.ListOutbox(context.Background(), id)
				return checkErr == nil && len(rows) == 1 && rows[0].State == "retryable"
			}, 10*time.Second, 20*time.Millisecond)
			require.NoError(t, h.iam.SetPositiveCacheTTL(0))
			p.mu.Lock()
			p.iamDown = outage
			p.userInactive = !outage
			p.mu.Unlock()
			if outage {
				require.Never(t, func() bool {
					p.mu.Lock()
					defer p.mu.Unlock()
					return len(p.sent) > 1
				}, 3*time.Second, 25*time.Millisecond)
				p.mu.Lock()
				p.iamDown = false
				p.mu.Unlock()
				h.awaitReplies(t, 2)
			} else {
				h.await(t, id, "delivery_failed")
			}
			h.stop(t)
			p.mu.Lock()
			defer p.mu.Unlock()
			if outage {
				require.Len(t, p.sent, 2)
			} else {
				require.Len(t, p.sent, 1)
				rows, checkErr := h.store.ListOutbox(context.Background(), id)
				require.NoError(t, checkErr)
				require.Equal(t, "delivery_blocked", rows[0].State)
			}
		})
	}
}

func TestAgentAccountUsesSubjectAndDeniesForeignTasksAndAdmin(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	h := setup(t, p)
	id := h.submit(t, 5)
	h.await(t, id, "completed")
	h.awaitReplies(t, 1)
	accepted, err := h.store.GetTask(context.Background(), id)
	require.NoError(t, err)
	user, err := h.store.GetUser(context.Background(), accepted.UserID)
	require.NoError(t, err)
	endpoint, err := api.New(
		h.store,
		h.iam,
		h.service,
		api.Options{
			BotID:          123,
			WebhookSecret:  "test-secret",
			OrganizationID: "org",
			ActorID:        "actor",
			Issuer:         p.iam.URL,
			LoomAudience:   "loom",
		},
	)
	require.NoError(t, err)
	server := httptest.NewServer(endpoint.InternalHandler())
	defer server.Close()
	call := func(method, path, subject, body string) (int, []byte) {
		req, e := http.NewRequest(method, server.URL+path, strings.NewReader(body))
		require.NoError(t, e)
		req.Header.Set("Authorization", "Bearer "+p.token(subject))
		req.Header.Set("Content-Type", "application/json")
		response, e := server.Client().Do(req)
		require.NoError(t, e)
		defer response.Body.Close()
		raw, e := io.ReadAll(response.Body)
		require.NoError(t, e)
		return response.StatusCode, raw
	}
	status, raw := call(
		http.MethodPost,
		"/internal/agent/account/me",
		user.Subject,
		`{"protocol_version":1,"operation_id":"`+uuid.NewString()+`","arguments":{}}`,
	)
	require.Equal(t, http.StatusOK, status, string(raw))
	require.Contains(t, string(raw), `"telegram_user_id":456`)
	foreign, _, err := h.store.Intake(
		context.Background(),
		db.IntakeRequest{
			BotID:          123,
			UpdateID:       999,
			TelegramUserID: 789,
			ChatID:         789,
			Profile:        json.RawMessage(`{}`),
			Payload:        json.RawMessage(`{"kind":"message","text":"foreign"}`),
		},
		nil,
	)
	require.NoError(t, err)
	_, err = h.store.BindSubject(context.Background(), foreign.UserID, "linked-foreign-user", "org")
	require.NoError(t, err)
	status, raw = call(http.MethodGet, "/internal/tasks/"+id.String(), "linked-foreign-user", "")
	require.GreaterOrEqual(t, status, 400, string(raw))
	require.NotContains(t, string(raw), "Hello")
	status, raw = call(http.MethodGet, "/internal/admin/settings", user.Subject, "")
	require.Equal(t, http.StatusForbidden, status, string(raw))
	_, err = h.store.SetUserRevoked(context.Background(), user.ID, "test-admin")
	require.NoError(t, err)
	h.iam.Invalidate(user.Subject)
	status, raw = call(
		http.MethodPost,
		"/internal/agent/account/me",
		user.Subject,
		`{"protocol_version":1,"operation_id":"`+uuid.NewString()+`","arguments":{}}`,
	)
	require.Equal(t, http.StatusForbidden, status, string(raw))
}

func TestQuestionCanBeAnsweredWhileOriginalRunIsWaiting(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.question = true
	h := setup(t, p)
	original := h.submit(t, 6)
	h.await(t, original, "awaiting_user")
	h.awaitReplies(t, 1)
	answer := h.submit(t, 7)
	h.await(t, answer, "completed")
	h.await(t, original, "completed")
	h.awaitReplies(t, 2)
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Len(t, p.runs, 1)
	require.Len(t, p.inputs, 1)
	require.Equal(t, "steering", p.inputs[0].Kind)
	require.Empty(t, p.inputs[0].QuestionID)
	require.Equal(t, "Hello", promptUserText(p.inputs[0].Text))
	require.Equal(t, "Thank you!", p.sent[1].Text)
}

func TestCancelTextReachesAgentWhileQuestionIsWaiting(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.question = true
	h := setup(t, p)
	original := h.submit(t, 8)
	h.await(t, original, "awaiting_user")
	h.awaitReplies(t, 1)
	cancellation := h.submitText(t, 9, "/cancel")
	require.Equal(t, cancellation, h.submitText(t, 9, "/cancel"))
	h.await(t, cancellation, "completed")
	h.await(t, original, "completed")
	h.awaitReplies(t, 2)
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Len(t, p.runs, 1)
	require.Empty(t, p.cancelKeys)
	require.Len(t, p.inputs, 1)
	require.Equal(t, "steering", p.inputs[0].Kind)
	require.Equal(t, "/cancel", promptUserText(p.inputs[0].Text))
}

func TestSecondInstanceCannotRecoverLiveSendAndCanStartAfterStop(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	entered := make(chan struct{})
	release := make(chan struct{})
	p.sendEntered = entered
	p.sendRelease = release
	var once sync.Once
	defer once.Do(func() { close(release) })
	h := setup(t, p)
	id := h.submit(t, 10)
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("first instance did not reach Telegram send")
	}
	agent, err := loom.New(p.loom.URL, p.loom.Client())
	require.NoError(t, err)
	tg, err := telegram.New(p.tg.URL, "123:test-token", p.tg.Client())
	require.NoError(t, err)
	second, err := task.New(h.store, h.iam, agent, tg, nil, slog.New(slog.DiscardHandler), task.Options{BotID: 123})
	require.NoError(t, err)
	require.Error(t, second.Start(context.Background()))
	once.Do(func() { close(release) })
	h.await(t, id, "completed")
	h.awaitReplies(t, 1)
	h.stop(t)
	require.NoError(t, second.Start(context.Background()))
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, second.Stop(ctx))
	delivered, err := h.store.GetTask(context.Background(), id)
	require.NoError(t, err)
	require.Equal(t, "completed", delivered.State)
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Len(t, p.sent, 1)
}

func TestSteeringKeepsAcceptedOrderWhenFirstLoomCallIsDelayed(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.steering = true
	entered := make(chan struct{})
	release := make(chan struct{})
	secondEntered := make(chan struct{})
	p.firstInputEntered = entered
	p.firstInputRelease = release
	p.secondInputEntered = secondEntered
	var once sync.Once
	defer once.Do(func() { close(release) })
	h := setup(t, p)
	original := h.submit(t, 11)
	h.await(t, original, "running")
	first := h.submitText(t, 12, "first")
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("first accepted steering did not reach Loom")
	}
	second := h.submitText(t, 13, "second")
	select {
	case <-secondEntered:
		t.Fatal("later accepted steering overtook the pending first operation")
	case <-time.After(200 * time.Millisecond):
	}
	once.Do(func() { close(release) })
	h.await(t, first, "completed")
	h.await(t, second, "completed")
	h.stop(t)
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Len(t, p.inputs, 2)
	require.Equal(t, "first", promptUserText(p.inputs[0].Text))
	require.Equal(t, "second", promptUserText(p.inputs[1].Text))
}

func TestFailedReceiptPersistenceBecomesUnknownWithoutRestart(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	h := setup(t, p)
	// Inject the critical database write failure after HTTP send succeeds. This
	// schema-local trigger leaves admission, reservation and reconciliation usable.
	_, err := h.store.Pool().
		Exec(context.Background(), `CREATE FUNCTION fail_successful_receipt() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.state='delivered' THEN RAISE EXCEPTION 'receipt write unavailable'; END IF; RETURN NEW; END $$; CREATE TRIGGER receipt_failure BEFORE UPDATE ON shuttle_outbox FOR EACH ROW EXECUTE FUNCTION fail_successful_receipt()`)
	require.NoError(t, err)
	id := h.submit(t, 14)
	h.awaitReplies(t, 1)
	h.await(t, id, "delivery_unknown")
	h.stop(t)
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Len(t, p.sent, 1)
}

func TestUncertainFirstChunkBlocksFollowingChunksDurably(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.result = strings.Repeat("x", 5000)
	p.loseSend = true
	h := setup(t, p)
	id := h.submit(t, 15)
	h.await(t, id, "delivery_unknown")
	h.awaitReplies(t, 1)
	h.stop(t)
	h.start(t)
	require.Equal(t, id, h.submit(t, 15))
	h.await(t, id, "delivery_unknown")
	h.stop(t)
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Len(t, p.sent, 1)
	require.Len(t, p.sent[0].Text, 4096)
}

func TestUnknownQuestionReceiptStillAllowsAgentInputAfterRestart(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.question = true
	p.loseSend = true
	h := setup(t, p)
	original := h.submit(t, 16)
	h.awaitReplies(t, 1)
	h.stop(t)
	h.start(t)
	cancellation := h.submitText(t, 17, "/cancel")
	h.await(t, cancellation, "completed")
	require.Eventually(t, func() bool {
		p.mu.Lock()
		defer p.mu.Unlock()
		for _, run := range p.runs {
			if run.Status == "completed" {
				return true
			}
		}
		return false
	}, 15*time.Second, 25*time.Millisecond)
	h.stop(t)
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Len(t, p.runs, 1)
	require.Empty(t, p.cancelKeys)
	require.Len(t, p.inputs, 1)
	require.Equal(t, "steering", p.inputs[0].Kind)
	questions := 0
	for _, message := range p.sent {
		if message.Text == "What is your name?" {
			questions++
		}
	}
	require.Equal(t, 1, questions)
	require.Equal(t, "run-"+original.String(), p.runs[original.String()].ID)
}

func TestAcceptedTextStartsOwnRunIfPriorRunFinishesBeforeControlAfterRestart(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.steering = true
	p.echoInput = true
	p.getEntered = make(chan struct{}, 4)
	release := make(chan struct{})
	p.getRelease = release
	var once sync.Once
	defer once.Do(func() { close(release) })
	h := setup(t, p)
	original := h.submitText(t, 18, "first message")
	h.await(t, original, "running")
	following := h.submitText(t, 19, "second message")
	// Both the original poll and the following control read are paused before
	// any steering mutation. Restart while their accepted tasks remain durable.
	for range 2 {
		select {
		case <-p.getEntered:
		case <-time.After(5 * time.Second):
			t.Fatal("accepted message did not reach the paused Loom read")
		}
	}
	h.stop(t)
	p.mu.Lock()
	run := p.runs[original.String()]
	run.Status = "completed"
	run.Result = "first message"
	run.Version++
	p.runs[original.String()] = run
	p.steering = false
	p.mu.Unlock()
	once.Do(func() { close(release) })
	h.start(t)
	require.Equal(t, following, h.submitText(t, 19, "second message"))
	h.await(t, original, "completed")
	h.await(t, following, "completed")
	h.awaitReplies(t, 2)
	h.stop(t)
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Len(t, p.runs, 2)
	require.Empty(t, p.inputs)
	require.Equal(t, []string{"first message", "second message"}, promptUserTexts(p.createdInputs))
	require.Equal(t, "run-"+following.String(), p.runs[following.String()].ID)
	results := []string{p.sent[0].Text, p.sent[1].Text}
	require.ElementsMatch(t, []string{"first message", "second message"}, results)
}

func TestRejectedSteeringStartsOwnRunAfterPriorRunCompletes(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		status  int
		unknown bool
	}{
		{"rate_limit", http.StatusTooManyRequests, false},
		{"completion_conflict", http.StatusConflict, false},
		{"auth_rejection", http.StatusUnauthorized, false},
		{"unknown_effect_before_auth_rejection", http.StatusUnauthorized, true},
		{"unknown_effect_before_rejection", http.StatusTooManyRequests, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p := newPeers(t)
			p.steering = true
			p.echoInput = true
			p.rejectInputAndFinish = true
			p.unknownInputBeforeRejection = tc.unknown
			p.rejectInputStatus = tc.status
			h := setup(t, p)
			original := h.submitText(t, 60, "first message")
			h.await(t, original, "running")
			following := h.submitText(t, 61, "second message")
			h.await(t, original, "completed")
			h.await(t, following, "completed")
			if tc.unknown {
				h.awaitReplies(t, 1)
			} else {
				h.awaitReplies(t, 2)
			}
			h.stop(t)
			p.mu.Lock()
			defer p.mu.Unlock()
			if tc.unknown {
				require.Equal(t, []string{"first message"}, promptUserTexts(p.createdInputs))
				require.Len(t, p.inputs, 1)
				require.Equal(t, "steering", p.inputs[0].Kind)
				require.Equal(t, "second message", promptUserText(p.inputs[0].Text))
			} else {
				require.Equal(t, []string{"first message", "second message"}, promptUserTexts(p.createdInputs))
				require.Empty(t, p.inputs)
				require.ElementsMatch(t,
					[]string{"first message", "second message"},
					[]string{p.sent[0].Text, p.sent[1].Text},
				)
			}
		})
	}
}

func TestLaterAcceptedMessageCannotOvertakePendingPromotion(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.steering = true
	p.echoInput = true
	p.getEntered = make(chan struct{}, 4)
	release := make(chan struct{})
	p.getRelease = release
	var once sync.Once
	defer once.Do(func() { close(release) })
	h := setup(t, p)
	original := h.submitText(t, 20, "message A")
	h.await(t, original, "running")
	pending := h.submitText(t, 21, "message B")
	for range 2 {
		select {
		case <-p.getEntered:
		case <-time.After(5 * time.Second):
			t.Fatal("pending message did not reach the paused Loom read")
		}
	}
	h.stop(t)
	// Keep B's processing delayed while allowing A to persist completion and C
	// to be accepted. The schema-local row lock injects a scheduling delay only.
	held, err := h.store.Pool().Begin(context.Background())
	require.NoError(t, err)
	defer held.Rollback(context.Background())
	var locked uuid.UUID
	require.NoError(
		t,
		held.QueryRow(context.Background(), "SELECT id FROM shuttle_tasks WHERE id=$1 FOR UPDATE", pending).
			Scan(&locked),
	)
	p.mu.Lock()
	run := p.runs[original.String()]
	run.Status = "completed"
	run.Result = "message A"
	run.Version++
	p.runs[original.String()] = run
	p.steering = false
	p.mu.Unlock()
	once.Do(func() { close(release) })
	h.start(t)
	h.await(t, original, "completed")
	h.awaitReplies(t, 1)
	later := h.submitText(t, 22, "message C")
	time.Sleep(200 * time.Millisecond)
	p.mu.Lock()
	createdBeforeRelease := append([]string(nil), p.createdInputs...)
	p.mu.Unlock()
	require.Equal(t, []string{"message A"}, promptUserTexts(createdBeforeRelease))
	require.NoError(t, held.Commit(context.Background()))
	h.await(t, pending, "completed")
	h.await(t, later, "completed")
	h.awaitReplies(t, 3)
	h.stop(t)
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Equal(t, []string{"message A", "message B", "message C"}, promptUserTexts(p.createdInputs))
	require.Len(t, p.runs, 3)
	require.Empty(t, p.inputs)
}

func TestLoomClosedListenerOutageRecoversWithoutManualAuthorizationGate(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	address := p.loom.Listener.Addr().String()
	p.loom.Close()
	h := setup(t, p)
	id := h.submit(t, 23)
	h.await(t, id, "awaiting_authorization")
	// The listener stays closed across several scheduler retries, as it does
	// during a service restart. No request could reach Loom in this interval.
	time.Sleep(4 * time.Second)
	waiting, err := h.store.GetTask(context.Background(), id)
	require.NoError(t, err)
	require.NotEqual(t, "auth_unknown", waiting.State)
	listener, err := net.Listen("tcp", address)
	require.NoError(t, err)
	restored := httptest.NewUnstartedServer(http.HandlerFunc(p.agent))
	require.NoError(t, restored.Listener.Close())
	restored.Listener = listener
	restored.Start()
	t.Cleanup(restored.Close)
	h.await(t, id, "completed")
	h.awaitReplies(t, 1)
	h.stop(t)
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Len(t, p.runs, 1)
	require.Len(t, p.sent, 1)
}

func (p *peers) delayInput(r *http.Request) bool {
	if !strings.HasSuffix(r.URL.Path, "/input") {
		return true
	}
	var input loom.InputRequest
	body, _ := io.ReadAll(r.Body)
	r.Body = io.NopCloser(bytes.NewReader(body))
	_ = json.Unmarshal(body, &input)
	p.mu.Lock()
	p.inputKeys = append(p.inputKeys, r.Header.Get("Idempotency-Key"))
	p.inputAttempts = append(p.inputAttempts, input)
	p.mu.Unlock()
	if promptUserText(input.Text) == "first" && p.firstInputEntered != nil {
		p.firstInputOnce.Do(func() { close(p.firstInputEntered) })
		select {
		case <-p.firstInputRelease:
		case <-r.Context().Done():
			if p.firstInputCanceled != nil {
				p.firstInputCancelOnce.Do(func() { close(p.firstInputCanceled) })
			}
			return false
		}
	}
	if promptUserText(input.Text) == "second" && p.secondInputEntered != nil {
		close(p.secondInputEntered)
	}
	return true
}

func (p *peers) waitGet(r *http.Request) bool {
	if r.Method == http.MethodGet && p.getRelease != nil {
		select {
		case p.getEntered <- struct{}{}:
		default:
		}
		select {
		case <-p.getRelease:
		case <-r.Context().Done():
			return false
		}
	}
	return true
}

func (p *peers) applyInput(w http.ResponseWriter, r *http.Request) {
	var input loom.InputRequest
	_ = json.NewDecoder(r.Body).Decode(&input)
	body, _ := json.Marshal(input)
	key := r.URL.Path + "\x1f" + r.Header.Get("Idempotency-Key")
	if receipt, ok := p.inputReceipts[key]; ok {
		if p.inputBodies[key] != string(body) {
			w.WriteHeader(http.StatusConflict)
			respond(w, map[string]any{"error": map[string]any{"code": "idempotency_conflict"}})
			return
		}
		respond(w, receipt)
		return
	}
	for runKey, run := range p.runs {
		if !strings.HasSuffix(r.URL.Path, "/runs/"+run.ID+"/input") {
			continue
		}
		if run.Status == "completed" || run.Status == "failed" || run.Status == "cancelled" {
			code := "run_terminal"
			if p.terminalInputConflict {
				code = "idempotency_conflict"
			}
			w.WriteHeader(http.StatusConflict)
			respond(w, map[string]any{"error": map[string]any{"code": code}})
			return
		}
		p.inputs = append(p.inputs, input)
		if !p.steering {
			run.Status = "completed"
		}
		p.updateInputPrompt(&run, input)
		run.Result = "Thank you!"
		if p.inputResult != nil {
			run.Result = *p.inputResult
		}
		run.Version++
		p.runs[runKey] = run
		receipt := loom.ControlReceipt{
			ID:      run.ID,
			Status:  run.Status,
			Version: run.Version,
			InputID: "input-" + key,
			Context: &loom.InputContext{Revision: int64(len(p.inputs)), SourceActor: "fixture"},
		}
		if p.inputReceipts == nil {
			p.inputReceipts = map[string]loom.ControlReceipt{}
			p.inputBodies = map[string]string{}
		}
		p.inputReceipts[key] = receipt
		p.inputBodies[key] = string(body)
		respond(w, receipt)
		return
	}
	w.WriteHeader(http.StatusNotFound)
}

func TestCallbackAcknowledgementDoesNotChangeDurableAcceptance(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	h := setup(t, p)
	original := h.submit(t, 1)
	h.await(t, original, "completed")
	h.awaitReplies(t, 1)
	p.mu.Lock()
	p.loseAck = true
	p.mu.Unlock()
	update := telegram.Update{ID: 2, CallbackQuery: &telegram.CallbackQuery{
		ID: "callback-2", From: telegram.User{ID: 456, FirstName: "Daniel"},
		Message: &telegram.Message{ID: 1, Chat: telegram.Chat{ID: 456, Type: "private"}},
		Data:    "a:" + original.String() + ":00000000:approve",
	}}
	body, err := json.Marshal(update)
	require.NoError(t, err)
	var acceptedID uuid.UUID
	for attempt := range 2 {
		request, requestErr := http.NewRequest(http.MethodPost, h.server.URL+"/telegram/webhook", bytes.NewReader(body))
		require.NoError(t, requestErr)
		request.Header.Set("X-Telegram-Bot-Api-Secret-Token", "test-secret")
		response, sendErr := h.server.Client().Do(request)
		require.NoError(t, sendErr)
		require.Equal(t, http.StatusAccepted, response.StatusCode)
		var receipt struct {
			ID        uuid.UUID `json:"task_id"`
			Duplicate bool      `json:"duplicate"`
		}
		require.NoError(t, json.NewDecoder(response.Body).Decode(&receipt))
		require.NoError(t, response.Body.Close())
		require.Equal(t, attempt == 1, receipt.Duplicate)
		if attempt == 0 {
			acceptedID = receipt.ID
		} else {
			require.Equal(t, acceptedID, receipt.ID)
		}
	}
	accepted, err := h.store.GetTask(context.Background(), acceptedID)
	require.NoError(t, err)
	require.Equal(t, acceptedID, accepted.ID)
	p.mu.Lock()
	require.Equal(t, []string{"callback-2", "callback-2"}, p.callbackAcks)
	require.Len(t, p.createdInputs, 1)
	p.mu.Unlock()
	update.ID = 3
	update.CallbackQuery.ID = "callback-3"
	update.CallbackQuery.Data = "invalid"
	body, err = json.Marshal(update)
	require.NoError(t, err)
	request, err := http.NewRequest(http.MethodPost, h.server.URL+"/telegram/webhook", bytes.NewReader(body))
	require.NoError(t, err)
	request.Header.Set("X-Telegram-Bot-Api-Secret-Token", "test-secret")
	response, err := h.server.Client().Do(request)
	require.NoError(t, err)
	defer response.Body.Close()
	require.Equal(t, http.StatusOK, response.StatusCode)
	var ignored map[string]any
	require.NoError(t, json.NewDecoder(response.Body).Decode(&ignored))
	require.Equal(t, true, ignored["ignored"])
	require.NotContains(t, ignored, "task_id")
	p.mu.Lock()
	require.Equal(t, []string{"callback-2", "callback-2", "callback-3"}, p.callbackAcks)
	p.mu.Unlock()
}

func (p *peers) authSession(w http.ResponseWriter, r *http.Request) {
	if strings.HasSuffix(r.URL.Path, "/reauthorize") {
		var request struct {
			Version int64 `json:"version"`
		}
		_ = json.NewDecoder(r.Body).Decode(&request)
		p.reauthorizationVersions = append(p.reauthorizationVersions, request.Version)
		p.sessionReauthorizations++
		if p.loseReauthorization {
			disconnect(w)
			return
		}
	}
	if r.Header.Get("X-Subject-Token") == "" {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	if p.loseAuth {
		disconnect(w)
		return
	}
	if strings.HasSuffix(r.URL.Path, "/reauthorize") && !p.keepWaitingAuth {
		for key, run := range p.runs {
			if run.Status == "waiting_auth" {
				run.Status, run.Reason, run.Result = "completed", "", "Recovered authorization"
				p.runs[key] = run
			}
		}
	}
	respond(
		w,
		loom.Session{
			ID:        "session",
			Version:   int64(1 + p.sessionReauthorizations),
			ExpiresAt: p.credentialExpiry(),
		},
	)
}

func (p *peers) rejectAgentRequest(w http.ResponseWriter, r *http.Request) bool {
	if p.loomDown {
		p.loomUnavailableCalls++
		w.WriteHeader(http.StatusServiceUnavailable)
		return true
	}
	if p.loomAuthDenied || r.Header.Get("Authorization") == "" {
		p.loomAuthDeniedCalls++
		w.WriteHeader(http.StatusUnauthorized)
		return true
	}
	return false
}

// Keep the accepted prompt in subsequent reads, as the real Loom API does.
func (p *peers) updateInputPrompt(run *loom.Run, input loom.InputRequest) {
	if !p.retainResolvedPrompts {
		run.Question = nil
		return
	}
	if input.Kind == "steering" && run.Question != nil {
		question := *run.Question
		question.Answer = &input
		run.Question = &question
	}
	if input.Kind == "approval" && run.Approval != nil {
		approval := *run.Approval
		approval.Decision = input.Decision
		run.Approval = &approval
		run.Status = "reconciling"
	}
}

// issueToken runs under the peer mutex, including the controlled IAM wait hook.
func (p *peers) issueToken(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	sub := "actor"
	if r.Form.Get("grant_type") != "client_credentials" {
		sub = r.Form.Get("subject_token")
	}
	if p.tokenHook != nil {
		p.tokenHook(sub)
	}
	if p.unavailableTokenSubjects[sub] {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	respond(w, map[string]any{"access_token": p.token(sub), "expires_in": 3600, "token_type": "Bearer"})
}
