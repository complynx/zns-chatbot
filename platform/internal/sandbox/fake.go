// Package sandbox is a local-only subset of the Telegram Bot API and a manual test UI.
package sandbox

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/complynx/zns-chatbot/platform/internal/api"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

//go:embed index.html
var page []byte

//go:embed app.js
var appScript []byte

const editMessageTextMethod = "editMessageText"

const errorField = "error"

type Fake struct {
	registrationIngress *registrationIngressControl
	callbackEvidence    callbackEvidence
	delay               *editDelay
	menu                telegramMenuState
	menuFailure         *menuFault
	modelFixtures       modelFixtures
	modelControl        *modelFixtureControl
	modelConsumed       []modelConsumption
	MiniAppURL          string
	mu                  sync.Mutex
	next                int64
	updates             []telegram.Update
	messages            []telegram.Message
	edits               int
	asrCalls            int
	fault               string
	blocked             map[int64]bool
	stickers            map[string]telegram.Sticker
	DB                  *pgxpool.Pool
	Token               string
}

type snapshot struct {
	RegistrationIngress *registrationIngressControl `json:"RegistrationIngress,omitempty"`
	ModelConsumed       *modelConsumptionSnapshot   `json:"ModelConsumed,omitempty"`
	Menu                telegramMenuState           `json:"Menu"`
	Stickers            map[string]telegram.Sticker `json:"Stickers,omitempty"`
	Blocked             map[int64]bool              `json:"Blocked,omitempty"`
	Next                int64                       `json:"Next"`
	Updates             []telegram.Update           `json:"Updates"`
	Messages            []telegram.Message          `json:"Messages"`
	Edits               int                         `json:"Edits"`
}

func New(ctx context.Context, db *pgxpool.Pool, token string) (*Fake, error) {
	f := &Fake{DB: db, Token: token, modelControl: newModelFixtureControl(ctx)}
	var raw []byte
	if db == nil {
		return f, f.enableEditDelay(ctx)
	}
	e := db.QueryRow(ctx, `SELECT data FROM bot.fake_state WHERE id=true`).Scan(&raw)
	if errors.Is(e, pgx.ErrNoRows) {
		return f, f.enableEditDelay(ctx)
	}
	if e != nil {
		return nil, e
	}
	var s snapshot
	if e = json.Unmarshal(raw, &s); e != nil {
		return nil, e
	}
	if e = f.restoreModelConsumption(s.ModelConsumed); e != nil {
		return nil, e
	}
	if e = validateRegistrationIngress(s.RegistrationIngress); e != nil {
		return nil, e
	}
	f.registrationIngress = s.RegistrationIngress
	f.next = s.Next
	f.updates = s.Updates
	f.messages = s.Messages
	f.edits = s.Edits
	f.blocked = s.Blocked
	f.stickers = s.Stickers
	f.menu = s.Menu
	return f, f.enableEditDelay(ctx)
}

// Save before acknowledging a mutation, so fake Telegram survives container restarts.
func (f *Fake) save(ctx context.Context) error {
	if f.DB == nil {
		return nil
	}
	raw, e := json.Marshal(
		snapshot{
			RegistrationIngress: f.registrationIngress,
			ModelConsumed:       f.modelConsumptionSnapshot(),
			Menu:                f.menu,
			Next:                f.next,
			Updates:             f.updates,
			Messages:            f.messages,
			Edits:               f.edits,
			Blocked:             f.blocked,
			Stickers:            f.stickers,
		},
	)
	if e != nil {
		return e
	}
	_, e = f.DB.Exec(
		ctx,
		`INSERT INTO bot.fake_state(id,data) VALUES(true,$1) ON CONFLICT(id) DO UPDATE SET data=$1`,
		raw,
	)
	return e
}

func (f *Fake) Handler() http.Handler {
	f.mu.Lock()
	if f.modelControl == nil {
		f.modelControl = newModelFixtureControl(context.Background())
	}
	f.mu.Unlock()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().
			Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'unsafe-inline'; frame-ancestors 'none'")
		_, _ = w.Write(page)
	})
	mux.HandleFunc("GET /app.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		_, _ = w.Write(appScript)
	})
	mux.HandleFunc(
		"GET /healthz",
		func(w http.ResponseWriter, _ *http.Request) { api.JSON(w, http.StatusOK, map[string]bool{"ok": true}) },
	)
	mux.HandleFunc("POST /{bot}/{method}", f.telegram)
	mux.HandleFunc("GET /lab/state", f.labState)
	mux.HandleFunc("GET /lab/callback-receipts", f.callbackReceiptState)
	mux.HandleFunc("POST /lab/input", f.labInput)
	mux.HandleFunc("POST /lab/registration-ingress", f.registrationIngressCommand)
	mux.HandleFunc("GET /lab/registration-ingress", f.registrationIngressRead)
	mux.HandleFunc("POST /lab/fault", f.labFault)
	mux.HandleFunc("POST /lab/blocked", f.labBlocked)
	mux.HandleFunc("POST /lab/asr", f.fixtureTranscribe)
	mux.HandleFunc("GET /lab/asr/state", f.fixtureASRState)
	f.menuRoutes(mux)
	f.miniAppRoutes(mux)
	f.documentRoutes(mux)
	f.stickerRoutes(mux)
	f.modelFixtureRoutes(mux)
	if f.delay != nil {
		return f.delay.dataHandler(mux)
	}
	return mux
}
func labRequest(w http.ResponseWriter, r *http.Request) bool {
	if r.Header.Get("X-Sandbox") != "1" {
		api.JSON(w, http.StatusForbidden, map[string]string{errorField: "sandbox header required"})
		return false
	}
	return true
}

const transientFault = "transient"

const (
	fakeBotID         = 999
	emptyPollDelay    = 200 * time.Millisecond
	maxPendingUpdates = 1000
	noFault           = "none"
)

func (f *Fake) telegram(w http.ResponseWriter, r *http.Request) {
	if r.PathValue("bot") != "bot"+f.Token {
		api.JSON(w, http.StatusUnauthorized, map[string]any{"ok": false})
		return
	}
	method := r.PathValue("method")
	switch method {
	case setMenuButtonMethod, setCommandsMethod, "getChatMenuButton", "getMyCommands":
		f.telegramMenu(w, r, method)
	case "getUpdates":
		f.getUpdates(w, r)
	case "answerCallbackQuery":
		f.answerCallbackQuery(w, r)
	case "getMe":
		tgOK(w, telegram.User{ID: fakeBotID, IsBot: true, FirstName: "Sandbox"})
	case "sendMessage", editMessageTextMethod:
		f.writeMessage(w, r, method)
	case "getFile":
		f.getFile(w, r)
	case "getCustomEmojiStickers":
		f.getCustomEmojiStickers(w, r)
	case "forwardMessage":
		f.forwardMessage(w, r)
	case "sendDocument":
		f.sendDocument(w, r)
	default:
		tgError(w, http.StatusNotFound, "unsupported method")
	}
}

func (f *Fake) getUpdates(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Offset  int64    `json:"offset"`
		Timeout int      `json:"timeout"`
		Allowed []string `json:"allowed_updates"`
	}
	if api.Decode(w, r, &in) != nil {
		tgError(w, http.StatusBadRequest, "invalid payload")
		return
	}
	f.mu.Lock()
	previousIngress := f.registrationIngress
	kept := []telegram.Update{}
	for _, u := range f.updates {
		if u.ID >= in.Offset {
			kept = append(kept, u)
		}
	}
	f.updates = kept
	// Bound encoded bytes too: JSON escaping can multiply text size by six.
	batch := []telegram.Update{}
	encodedSize := 256 // Bot API envelope and separators.
	for _, u := range kept {
		raw, _ := json.Marshal(u)
		if len(batch) >= 100 || encodedSize+len(raw)+1 > 1<<20 {
			break
		}
		batch = append(batch, u)
		encodedSize += len(raw) + 1
	}
	response, err := f.registrationIngressResponse(batch)
	if err == nil {
		err = f.save(r.Context())
	}
	if err != nil {
		f.registrationIngress = previousIngress
	}
	f.mu.Unlock()
	if err != nil {
		tgError(w, http.StatusServiceUnavailable, "state unavailable")
		return
	}
	if response != nil {
		var envelope struct {
			Result []telegram.Update `json:"result"`
		}
		if json.Unmarshal(response, &envelope) != nil {
			tgError(w, http.StatusServiceUnavailable, "state unavailable")
			return
		}
		if len(envelope.Result) == 0 {
			select {
			case <-r.Context().Done():
				return
			case <-time.After(emptyPollDelay):
			}
		}
		f.observeDeliveredCallbacks(envelope.Result)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(response)
		return
	}
	if len(batch) == 0 {
		select {
		case <-r.Context().Done():
			return
		case <-time.After(emptyPollDelay):
		}
	}
	f.observeDeliveredCallbacks(batch)
	tgOK(w, batch)
}

func (f *Fake) writeMessage(w http.ResponseWriter, r *http.Request, method string) {
	var wire deliveryRequest
	digest := sha256.New()
	if f.delay != nil {
		r.Body = &delayBody{Reader: io.TeeReader(r.Body, digest), Closer: r.Body}
	}
	if api.Decode(w, r, &wire) != nil {
		tgError(w, http.StatusBadRequest, "invalid payload")
		return
	}
	p, chat, err := wire.resolve()
	if err != nil {
		tgError(w, http.StatusBadRequest, err.Error())
		return
	}
	text, entities, textErr := deliveryText(p)
	if textErr != nil {
		tgError(w, http.StatusBadRequest, "invalid message text or entities")
		return
	}
	p.Text = text
	in := admittedDelivery{wire: wire, send: p, chat: chat, entities: entities}
	if f.delay != nil && method == editMessageTextMethod {
		held, selectErr := f.delay.selectEdit(wire, hex.EncodeToString(digest.Sum(nil)))
		if selectErr != nil {
			tgError(w, http.StatusServiceUnavailable, "synthetic evidence unavailable")
			return
		}
		if held {
			f.delay.apply(w, r, in, method, f.writeAdmittedMessage)
			return
		}
	}
	f.writeAdmittedMessage(w, r, in, method)
}

type admittedDelivery struct {
	wire     deliveryRequest
	send     telegram.Send
	chat     telegram.Chat
	entities []telegram.MessageEntity
}

// Carry the ordinary nonmutating admission through the optional hold unchanged.
func (f *Fake) writeAdmittedMessage(w http.ResponseWriter, r *http.Request, in admittedDelivery, method string) {
	p, chat, entities := in.send, in.chat, in.entities
	if err := f.delayMutationLock(r.Context()); err != nil {
		tgError(w, http.StatusServiceUnavailable, "synthetic apply unavailable")
		return
	}
	defer f.mu.Unlock()
	if f.blocked[p.ChatID] {
		tgError(w, http.StatusForbidden, "bot was blocked by the user")
		return
	}
	if f.fault == transientFault {
		f.fault = noFault
		tgError(w, http.StatusTooManyRequests, "Too Many Requests")
		return
	}
	if method == editMessageTextMethod {
		f.editMessage(w, r, p, entities)
		return
	}
	f.next++
	m := telegram.Message{
		ID:       f.next,
		Chat:     chat,
		ThreadID: in.wire.ThreadID,
		From:     telegram.User{ID: fakeBotID, IsBot: true},
		Text:     p.Text,
		Entities: entities,
		Markup:   p.Markup,
	}
	f.messages = append(f.messages, m)
	if e := f.save(r.Context()); e != nil {
		tgError(w, http.StatusServiceUnavailable, "state unavailable")
		return
	}
	tgOK(w, m)
}

func (f *Fake) editMessage(w http.ResponseWriter, r *http.Request, p telegram.Send, entities []telegram.MessageEntity) {
	if f.fault == "edit_missing" {
		f.fault = noFault
		for i, m := range f.messages {
			if m.ID == p.MessageID && m.Chat.ID == p.ChatID {
				f.messages = append(f.messages[:i], f.messages[i+1:]...)
				break
			}
		}
		if e := f.save(r.Context()); e != nil {
			tgError(w, http.StatusServiceUnavailable, "state unavailable")
			return
		}
		tgError(w, http.StatusBadRequest, "Bad Request: message to edit not found")
		return
	}
	for i, m := range f.messages {
		if m.ID == p.MessageID && m.Chat.ID == p.ChatID {
			f.messages[i].Text = p.Text
			f.messages[i].Entities = entities
			f.messages[i].Markup = p.Markup
			f.edits++
			if e := f.save(r.Context()); e != nil {
				tgError(w, http.StatusServiceUnavailable, "state unavailable")
				return
			}
			tgOK(w, f.messages[i])
			return
		}
	}
	tgError(w, http.StatusBadRequest, "Bad Request: message to edit not found")
}

func tgOK(w http.ResponseWriter, v any) {
	api.JSON(w, http.StatusOK, map[string]any{"ok": true, "result": v})
}
func tgError(w http.ResponseWriter, code int, text string) {
	api.JSON(w, code, map[string]any{"ok": false, "error_code": code, "description": text})
}

// RequireSandbox restricts HTTP surfaces to the isolated sandbox deployment.
func RequireSandbox(env string) error {
	if strings.TrimSpace(env) != "sandbox" {
		return fmt.Errorf("stage 1 binaries require ZNS_ENV=sandbox; production identity is not implemented")
	}
	return nil
}
func Ready(ctx context.Context, url string) error {
	r, e := http.NewRequestWithContext(ctx, http.MethodGet, url+"/healthz", nil)
	if e != nil {
		return e
	}
	resp, e := http.DefaultClient.Do(r)
	if e != nil {
		return e
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unhealthy: %d", resp.StatusCode)
	}
	return nil
}
func (f *Fake) labState(w http.ResponseWriter, r *http.Request) {
	uid, _ := strconv.ParseInt(r.URL.Query().Get("user"), 10, 64)
	owner, ok := identity.Subject(uid)
	_, destinationOK := destination(strconv.FormatInt(uid, 10))
	if !ok && !destinationOK {
		api.JSON(w, http.StatusBadRequest, nil)
		return
	}
	var processedCursor int64
	if f.DB != nil {
		if err := f.DB.QueryRow(r.Context(), `SELECT COALESCE((SELECT value FROM bot.cursors WHERE name='telegram'),0)`).
			Scan(&processedCursor); err != nil {
			api.JSON(w, http.StatusServiceUnavailable, map[string]string{errorField: "cursor unavailable"})
			return
		}
	}
	f.mu.Lock()
	messages := []telegram.Message{}
	for _, m := range f.messages {
		if m.Chat.ID == uid {
			messages = append(messages, m)
		}
	}
	edits := f.edits
	stickers := f.ownerStickers(messages)
	fault := f.fault
	f.mu.Unlock()
	events := []json.RawMessage{}
	if f.DB != nil && ok {
		rows, e := f.DB.Query(
			r.Context(),
			`SELECT jsonb_build_object('id',id,'update_id',update_id,'kind',kind,'content',content)
			FROM (SELECT id,update_id,kind,content FROM bot.interactions WHERE owner=$1 ORDER BY id DESC LIMIT 40) t ORDER BY id`,
			owner,
		)
		if e != nil {
			api.JSON(w, http.StatusServiceUnavailable, map[string]string{errorField: "history unavailable"})
			return
		}
		for rows.Next() {
			var raw []byte
			if e = rows.Scan(&raw); e != nil {
				rows.Close()
				api.JSON(w, http.StatusServiceUnavailable, nil)
				return
			}
			events = append(events, json.RawMessage(raw))
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			api.JSON(w, http.StatusServiceUnavailable, nil)
			return
		}
	}
	api.JSON(
		w,
		http.StatusOK,
		map[string]any{
			"messages":         messages,
			"stickers":         stickers,
			"edits":            edits,
			"history":          events,
			"fault":            fault,
			"processed_cursor": processedCursor,
		},
	)
}
func (f *Fake) labInput(w http.ResponseWriter, r *http.Request) {
	if !labRequest(w, r) {
		return
	}
	var in struct {
		User          int64  `json:"user"`
		Username      string `json:"username"`
		LanguageCode  string `json:"language_code"`
		Text          string `json:"text"`
		Data          string `json:"data"`
		MessageID     int64  `json:"message_id"`
		ContactID     int64  `json:"contact_id"`
		ForwardUserID int64  `json:"forward_user_id"`
		HiddenSender  bool   `json:"hidden_sender"`
	}
	if api.Decode(w, r, &in) != nil || len(in.Text) > 5000 || len(in.Data) > 64 || len(in.LanguageCode) > 64 ||
		len(in.Username) > 64 {
		api.JSON(w, http.StatusBadRequest, nil)
		return
	}
	if _, ok := identity.Subject(in.User); !ok {
		api.JSON(w, http.StatusBadRequest, nil)
		return
	}
	if !validLabIdentity(in.ContactID, in.ForwardUserID, in.HiddenSender, in.Data) {
		api.JSON(w, http.StatusBadRequest, nil)
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.updates) >= maxPendingUpdates {
		api.JSON(w, http.StatusTooManyRequests, nil)
		return
	}
	f.next++
	u := telegram.Update{ID: f.next}
	from := telegram.User{
		ID:           in.User,
		Username:     in.Username,
		FirstName:    strconv.FormatInt(in.User, 10),
		LanguageCode: in.LanguageCode,
	}
	if in.Data != "" {
		var msg telegram.Message
		for _, m := range f.messages {
			if m.ID == in.MessageID && m.Chat.ID == in.User {
				msg = m
			}
		}
		if msg.ID == 0 {
			api.JSON(w, http.StatusBadRequest, nil)
			return
		}
		u.Callback = &telegram.Callback{ID: strconv.FormatInt(f.next, 10), From: from, Data: in.Data, Message: msg}
	} else {
		u.Message = &telegram.Message{
			ID:   f.next,
			From: from,
			Chat: telegram.Chat{ID: in.User, Type: privateChat},
			Text: in.Text,
		}
		setLabIdentity(u.Message, in.ContactID, in.ForwardUserID, in.HiddenSender)
		f.messages = append(f.messages, *u.Message)
	}
	f.updates = append(f.updates, u)
	if e := f.save(r.Context()); e != nil {
		api.JSON(w, http.StatusServiceUnavailable, nil)
		return
	}
	api.JSON(w, http.StatusOK, u)
}
func (f *Fake) labFault(w http.ResponseWriter, r *http.Request) {
	if !labRequest(w, r) {
		return
	}
	var in struct {
		Mode string `json:"mode"`
	}
	if api.Decode(w, r, &in) != nil || in.Mode != noFault && in.Mode != "edit_missing" && in.Mode != transientFault {
		api.JSON(w, http.StatusBadRequest, nil)
		return
	}
	f.mu.Lock()
	f.fault = in.Mode
	f.mu.Unlock()
	api.JSON(w, http.StatusOK, in)
}
