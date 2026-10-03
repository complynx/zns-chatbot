package sandbox

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	delayStateArmed         = "armed"
	delayStateReleasing     = "releasing"
	delayModeBefore         = "before_apply"
	delayStateReleased      = "released_unresolved"
	delayStateInvalidated   = "invalidated"
	delayModeAfter          = "after_apply_loss"
	delayLockPoll           = 10 * time.Millisecond
	delayMinimumKeyBytes    = 24
	delayDataConnections    = 4
	delayControlConnections = 2
	delayHeaderDeadline     = 2 * time.Second
	delayReadDeadline       = 5 * time.Second
	delayControlBodyLimit   = 1024
	delayJournalMode        = 0600
)

const delayLifetime = 600 * time.Second
const delayApplyLifetime = 20 * time.Second
const delayResponseLimit = 2 * 1024 * 1024

type delayApplyKey struct{}

// Ordinary calls retain their existing mutex/save lifecycle. Only a selected
// synthetic call waits with its provider deadline/invalidation, with no worker.
func (f *Fake) delayMutationLock(ctx context.Context) error {
	d, selected := ctx.Value(delayApplyKey{}).(*editDelay)
	if !selected {
		f.mu.Lock()
		return nil
	}
	canceled := func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		select {
		case <-d.invalidated:
			return errors.New("synthetic case invalidated")
		default:
			return nil
		}
	}
	ticker := time.NewTicker(delayLockPoll)
	defer ticker.Stop()
	for {
		if err := canceled(); err != nil {
			return err
		}
		if f.mu.TryLock() {
			if err := canceled(); err != nil {
				f.mu.Unlock()
				return err
			}
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-d.invalidated:
			return errors.New("synthetic case invalidated")
		case <-ticker.C:
		}
	}
}

type delayBody struct {
	io.Reader
	io.Closer
}

type delayArm struct {
	ChatID     int64  `json:"chat_id"`
	MessageID  int64  `json:"message_id"`
	TextSHA256 string `json:"text_sha256"`
	Mode       string `json:"mode"`
}

type delayEvent struct {
	Sequence       int    `json:"sequence"`
	TimeNS         int64  `json:"time_ns"`
	Kind           string `json:"kind"`
	RequestSHA256  string `json:"request_sha256,omitempty"`
	ResponseSHA256 string `json:"response_sha256,omitempty"`
	ChatID         int64  `json:"chat_id,omitempty"`
	MessageID      int64  `json:"message_id,omitempty"`
	Status         int    `json:"status,omitempty"`
	KnownSuccess   bool   `json:"known_success,omitempty"`
}

// editDelay belongs only to the independently running synthetic provider.
// It never changes a product receipt, queue, admission, or acknowledgement.
type editDelay struct {
	armGuard      func() (func(), bool)
	modelControl  *modelFixtureControl
	mu            sync.Mutex
	arm           delayArm
	state         string
	requestSHA256 string
	release       chan struct{}
	ctx           context.Context
	key           string
	journal       *delayJournal
	events        []delayEvent
	dataSlots     chan struct{}
	invalidated   chan struct{}
	invalidOnce   sync.Once
}

// No environment opt-in means the original fake has no control listener.
func (f *Fake) enableEditDelay(ctx context.Context) error {
	key, enabled := os.LookupEnv("R104_CONTROL_KEY")
	if !enabled {
		return nil
	}
	if len(key) < delayMinimumKeyBytes {
		return errors.New("R104 requires a separate control secret of at least 24 bytes")
	}
	journal, err := openDelayJournal(os.Getenv("R104_JOURNAL"))
	if err != nil {
		return err
	}
	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", "0.0.0.0:8090")
	if err != nil {
		_ = journal.Close()
		return err
	}
	d := &editDelay{
		armGuard:     f.providerFaultEditGuard,
		modelControl: f.modelControl,
		state:        delayStateIdle,
		release:      make(chan struct{}),
		ctx:          ctx,
		key:          key,
		journal:      newDelayJournal(ctx, journal),
		dataSlots:    make(chan struct{}, delayDataConnections),
		invalidated:  make(chan struct{}),
	}
	f.delay = d
	server := &http.Server{
		Handler:           http.HandlerFunc(d.control),
		ReadHeaderTimeout: delayHeaderDeadline,
		ReadTimeout:       delayReadDeadline,
		WriteTimeout:      delayHeaderDeadline,
		IdleTimeout:       delayHeaderDeadline,
		MaxHeaderBytes:    delayControlBodyLimit,
	}
	bounded := &delayListener{Listener: listener, ctx: ctx, slots: make(chan struct{}, delayControlConnections)}
	go func() {
		<-ctx.Done()
		_ = server.Close()
	}()
	go func() {
		_ = server.Serve(bounded)
		d.invalidate()
	}()
	return nil
}

// DelayListener bounds the real fake listener before net/http starts header readers.
// Without the optional synthetic opt-in it returns the existing listener unchanged.
func (f *Fake) DelayListener(ctx context.Context, listener net.Listener) net.Listener {
	if f.delay == nil {
		return listener
	}
	return &delayListener{Listener: listener, ctx: ctx, slots: make(chan struct{}, delayDataConnections)}
}

// Bound active provider handlers separately from the reserved control connections.
// Root also binds the existing data server's header/body ingress deadlines.
func (d *editDelay) dataHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case d.dataSlots <- struct{}{}:
			defer func() { <-d.dataSlots }()
		default:
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// Bound connections before HTTP starts a header-reading goroutine.
type delayListener struct {
	net.Listener

	ctx   context.Context
	slots chan struct{}
}

func (l *delayListener) Accept() (net.Conn, error) {
	select {
	case l.slots <- struct{}{}:
	case <-l.ctx.Done():
		return nil, l.ctx.Err()
	}
	c, err := l.Listener.Accept()
	if err != nil {
		<-l.slots
		return nil, err
	}
	return &delayConnection{Conn: c, release: func() { <-l.slots }}, nil
}

type delayConnection struct {
	net.Conn

	once    sync.Once
	release func()
}

func (c *delayConnection) Close() error {
	err := c.Conn.Close()
	c.once.Do(c.release)
	return err
}

// Selection follows the real Go mux, token check, and api.Decode exactly once.
// Numeric-string aliases remain provider inputs but cannot select this private-chat hold.
func (d *editDelay) selectEdit(wire deliveryRequest, digest string) (bool, error) {
	var chat int64
	if json.Unmarshal(wire.ChatID, &chat) != nil || chat <= 0 || wire.MessageID <= 0 {
		return false, nil
	}
	textDigest := sha256.Sum256([]byte(wire.Text))
	d.mu.Lock()
	if d.state != delayStateArmed || chat != d.arm.ChatID || wire.MessageID != d.arm.MessageID ||
		hex.EncodeToString(textDigest[:]) != d.arm.TextSHA256 {
		d.mu.Unlock()
		return false, nil
	}
	d.state = "receiving"
	d.mu.Unlock()
	if err := d.record(delayEvent{Kind: "received", RequestSHA256: digest,
		ChatID: chat, MessageID: wire.MessageID}); err != nil {
		return false, err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.state != "receiving" {
		return false, errors.New("synthetic case invalidated")
	}
	d.requestSHA256 = digest
	if d.arm.Mode == delayModeBefore {
		d.state = "held_before_apply"
	} else {
		d.state = "forwarding_unresolved"
	}
	return true, nil
}

func (d *editDelay) waitRelease() bool {
	timer := time.NewTimer(delayLifetime)
	defer timer.Stop()
	select {
	case <-d.release:
		d.mu.Lock()
		valid := d.state == delayStateReleased
		d.mu.Unlock()
		return valid
	case <-timer.C:
		d.mu.Lock()
		if d.state != delayStateInvalidated {
			d.state = "expired_unresolved"
		}
		d.mu.Unlock()
		_ = d.record(delayEvent{Kind: "hold_expired"})
	case <-d.ctx.Done():
		d.invalidate()
	case <-d.invalidated:
		return false
	}
	return false
}

// Capture only the actual response generated after the fake's ordinary save.
type delayResponse struct {
	header http.Header
	status int
	body   []byte
}

func (w *delayResponse) Header() http.Header { return w.header }
func (w *delayResponse) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
}
func (w *delayResponse) Write(p []byte) (int, error) {
	if len(w.body)+len(p) > delayResponseLimit {
		return 0, errors.New("synthetic response too large")
	}
	if w.status == 0 {
		w.status = http.StatusOK
	}
	w.body = append(w.body, p...)
	return len(p), nil
}

func (d *editDelay) apply(w http.ResponseWriter, r *http.Request, in admittedDelivery, method string,
	apply func(http.ResponseWriter, *http.Request, admittedDelivery, string)) {
	if d.arm.Mode == delayModeBefore && !d.waitRelease() {
		panic(http.ErrAbortHandler)
	}
	if err := d.record(delayEvent{Kind: "forward_started", RequestSHA256: d.requestSHA256}); err != nil {
		panic(http.ErrAbortHandler)
	}
	// Provider work survives the caller, but is bounded and ends with the provider.
	ctx, cancel := context.WithTimeout(d.ctx, delayApplyLifetime)
	defer cancel()
	ctx = context.WithValue(ctx, delayApplyKey{}, d)
	if ctx.Err() != nil {
		panic(http.ErrAbortHandler)
	}
	response := &delayResponse{header: make(http.Header)}
	apply(response, r.WithContext(ctx), in, method)
	known, err := d.recordResponse(response)
	err = d.completeResponse(known, err)
	if err != nil {
		panic(http.ErrAbortHandler)
	}
	if known && d.arm.Mode == delayModeAfter {
		if !d.waitRelease() {
			panic(http.ErrAbortHandler)
		}
		err = d.record(delayEvent{Kind: "response_deliberately_lost", RequestSHA256: d.requestSHA256})
		d.mu.Lock()
		if err == nil && d.state != delayStateInvalidated {
			d.state = "response_lost"
		}
		d.mu.Unlock()
		// net/http closes the HTTP/1 connection (or resets the HTTP/2 stream) without an envelope.
		panic(http.ErrAbortHandler)
	}
	for key, values := range response.header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	w.WriteHeader(response.status)
	_, _ = w.Write(response.body)
}

func (d *editDelay) control(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if subtle.ConstantTimeCompare([]byte(r.Header.Get("X-R104-Control")), []byte(d.key)) != 1 {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, delayControlBodyLimit)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		w.WriteHeader(http.StatusRequestEntityTooLarge)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/control/model/") && d.modelControl != nil {
		d.modelControl.control(w, r, body)
		return
	}
	if r.Method == http.MethodGet && r.URL.Path == "/control/state" {
		d.mu.Lock()
		snapshot := struct {
			State  string       `json:"state"`
			Events []delayEvent `json:"events"`
		}{d.state, append([]delayEvent(nil), d.events...)}
		d.mu.Unlock()
		_ = json.NewEncoder(w).Encode(snapshot)
		return
	}
	if r.Method == http.MethodPost && r.URL.Path == "/control/arm" {
		d.controlArm(w, body)
		return
	}
	if r.Method == http.MethodPost && r.URL.Path == "/control/release" {
		d.controlRelease(w)
		return
	}
	w.WriteHeader(http.StatusConflict)
}

func openDelayJournal(path string) (*os.File, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, errors.New("R104 journal requires a clean absolute path")
	}
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	defer root.Close()
	return root.OpenFile(filepath.Base(path), os.O_WRONLY|os.O_CREATE|os.O_EXCL, delayJournalMode)
}

func (d *editDelay) controlArm(w http.ResponseWriter, body []byte) {
	var arm delayArm
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	decodeErr := decoder.Decode(&arm)
	var tail any
	digest, digestErr := hex.DecodeString(arm.TextSHA256)
	if decodeErr != nil || decoder.Decode(&tail) != io.EOF || arm.ChatID <= 0 || arm.MessageID <= 0 ||
		digestErr != nil || len(digest) != sha256.Size || hex.EncodeToString(digest) != arm.TextSHA256 ||
		(arm.Mode != delayModeBefore && arm.Mode != delayModeAfter) {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if d.armGuard != nil {
		release, allowed := d.armGuard()
		if !allowed {
			w.WriteHeader(http.StatusConflict)
			return
		}
		defer release()
	}
	d.mu.Lock()
	if d.state != delayStateIdle {
		d.mu.Unlock()
		w.WriteHeader(http.StatusConflict)
		return
	}
	d.state = "arming"
	d.mu.Unlock()
	if d.record(delayEvent{Kind: delayStateArmed, ChatID: arm.ChatID, MessageID: arm.MessageID}) != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	d.mu.Lock()
	if d.state != "arming" {
		d.mu.Unlock()
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	d.arm, d.state = arm, delayStateArmed
	d.mu.Unlock()
	_ = json.NewEncoder(w).Encode(map[string]string{"state": delayStateArmed})
}

func (d *editDelay) controlRelease(w http.ResponseWriter) {
	d.mu.Lock()
	if d.state != "held_before_apply" && d.state != "applied_response_held" {
		d.mu.Unlock()
		w.WriteHeader(http.StatusConflict)
		return
	}
	d.state = delayStateReleasing
	d.mu.Unlock()
	if d.record(delayEvent{Kind: "release_accepted"}) != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	d.mu.Lock()
	if d.state != "releasing" {
		d.mu.Unlock()
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	d.state = delayStateReleased
	close(d.release)
	d.mu.Unlock()
	_ = json.NewEncoder(w).Encode(map[string]string{"state": delayStateReleased})
}

func (d *editDelay) recordResponse(response *delayResponse) (bool, error) {
	digest := sha256.Sum256(response.body)
	var envelope struct {
		OK     bool `json:"ok"`
		Result struct {
			MessageID int64 `json:"message_id"`
			Chat      struct {
				ID int64 `json:"id"`
			} `json:"chat"`
		} `json:"result"`
	}
	known := response.status == http.StatusOK && json.Unmarshal(response.body, &envelope) == nil &&
		envelope.OK && envelope.Result.MessageID == d.arm.MessageID && envelope.Result.Chat.ID == d.arm.ChatID
	err := d.record(delayEvent{Kind: "upstream_response", RequestSHA256: d.requestSHA256,
		ResponseSHA256: hex.EncodeToString(digest[:]), Status: response.status})
	if err == nil {
		err = d.record(delayEvent{Kind: "held_outcome", KnownSuccess: known})
	}
	return known, err
}

func (d *editDelay) completeResponse(known bool, err error) error {
	d.mu.Lock()
	if d.state == delayStateInvalidated {
		err = errors.New("synthetic case invalidated")
	}
	if err == nil {
		switch {
		case known && d.arm.Mode == delayModeAfter:
			d.state = "applied_response_held"
		case known:
			d.state = "completed"
		default:
			d.state = "unresolved_response"
		}
	}
	d.mu.Unlock()
	return err
}
