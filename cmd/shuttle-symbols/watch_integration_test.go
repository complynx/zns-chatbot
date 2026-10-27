package main

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/loom"
	"github.com/complynx/zns-chatbot/internal/symboldescribe"
	"github.com/complynx/zns-chatbot/internal/symbols"
	"github.com/complynx/zns-chatbot/internal/telegram"
)

type descriptorPeers struct {
	mu                                 sync.Mutex
	chats, uploads, creates, firstGets int
	complete, loseUpload               bool
}

func (p *descriptorPeers) serve(w http.ResponseWriter, r *http.Request) {
	p.mu.Lock()
	defer p.mu.Unlock()
	var value any
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/v1/capabilities":
		value = map[string]any{"execution_profiles": []string{loom.NativeFilesProfile}}
	case r.Method == http.MethodPost && r.URL.Path == "/v1/chats":
		p.chats++
		value = map[string]any{"id": uuid.NewString()}
	case r.Method == http.MethodPost && r.URL.Path == "/v1/artifacts":
		_, _ = io.Copy(io.Discard, r.Body)
		p.uploads++
		if p.loseUpload {
			p.loseUpload = false
			conn, _, err := w.(http.Hijacker).Hijack()
			if err == nil {
				_ = conn.Close()
			}
			return
		}
		value = map[string]any{"artifact_id": uuid.NewString()}
	case r.Method == http.MethodPost && r.URL.Path == "/v1/runs":
		p.creates++
		id := "second"
		if p.creates == 1 {
			id = "first"
		} else if p.creates > 2 {
			id = uuid.NewString()
		}
		value = loom.Run{ID: id, Status: "running"}
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v1/runs/"):
		id := strings.TrimPrefix(r.URL.Path, "/v1/runs/")
		if id == "first" {
			p.firstGets++
		}
		state := "running"
		if p.complete {
			state = "completed"
		}
		value = loom.Run{ID: id, Status: state,
			Result: `{"description":"A smiling cat","category":"emotion","reaction_hint":"Possible happiness"}`}
	default:
		w.WriteHeader(http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}

func descriptorStore(t *testing.T) *db.Store {
	t.Helper()
	dsn := os.Getenv("SHUTTLE_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("SHUTTLE_TEST_DATABASE_URL required for isolated PostgreSQL")
	}
	admin, err := db.Open(t.Context(), dsn)
	require.NoError(t, err)
	schema := "descriptor_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	_, err = admin.Pool().Exec(t.Context(), "CREATE SCHEMA "+schema)
	require.NoError(t, err)
	u, err := url.Parse(dsn)
	require.NoError(t, err)
	query := u.Query()
	query.Set("search_path", schema)
	u.RawQuery = query.Encode()
	store, err := db.Open(t.Context(), u.String())
	require.NoError(t, err)
	t.Cleanup(func() {
		store.Close()
		_, _ = admin.Pool().Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
		admin.Close()
	})
	require.NoError(t, store.Migrate(t.Context()))
	return store
}

func descriptorDependencies(t *testing.T, p *descriptorPeers) dependencies {
	t.Helper()
	store := descriptorStore(t)
	var pngBytes bytes.Buffer
	require.NoError(t, png.Encode(&pngBytes, image.NewRGBA(image.Rect(0, 0, 1, 1))))
	tgServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/getFile") {
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": map[string]any{
				"file_id": "original", "file_path": "original.png", "file_size": pngBytes.Len(),
			}})
			return
		}
		_, _ = w.Write(pngBytes.Bytes())
	}))
	t.Cleanup(tgServer.Close)
	tg, err := telegram.New(tgServer.URL, "123:test", tgServer.Client())
	require.NoError(t, err)
	loomServer := httptest.NewServer(http.HandlerFunc(p.serve))
	t.Cleanup(loomServer.Close)
	lc, err := loom.New(loomServer.URL, loomServer.Client())
	require.NoError(t, err)
	return dependencies{store: store, flow: symboldescribe.Flow{Telegram: tg, Loom: lc}, bot: 1,
		auth: loom.Auth{ServiceToken: "synthetic-actor", SubjectToken: "synthetic-descriptor"}}
}

func seedDescriptor(t *testing.T, d dependencies, id string, generation uuid.UUID) {
	t.Helper()
	ref := symbols.Reference{Kind: symbols.Sticker, ID: id, FileID: "original"}
	raw, err := json.Marshal(ref)
	require.NoError(t, err)
	_, err = d.store.Pool().Exec(t.Context(), `INSERT INTO shuttle_symbols
(bot_id,kind,asset_key,generation,reference,score,last_used_at)
VALUES(1,'sticker',$1,$2,$3,1,now())`, id, generation, raw)
	require.NoError(t, err)
}

func startDescriptor(t *testing.T, a arguments, d dependencies) func() {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		done <- watchQueue(ctx, a, d, func(ctx context.Context, a arguments) error {
			err := runEntry(ctx, a, d)
			if err != nil && ctx.Err() == nil {
				t.Logf("descriptor step outcome: %v", err)
			}
			return err
		},
			10*time.Millisecond)
	}()
	var once sync.Once
	stop := func() {
		once.Do(func() {
			cancel()
			select {
			case err := <-done:
				require.NoError(t, err)
			case <-time.After(5 * time.Second):
				t.Fatal("descriptor did not stop")
			}
		})
	}
	t.Cleanup(stop)
	return stop
}

func TestDescriptorWatchRestartsOriginalRunAfterCatalogEviction(t *testing.T) {
	t.Parallel()
	p := &descriptorPeers{}
	d := descriptorDependencies(t, p)
	first := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	second := uuid.MustParse("00000000-0000-0000-0000-000000000002")
	seedDescriptor(t, d, "first", first)
	seedDescriptor(t, d, "second", second)
	a := arguments{journal: t.TempDir()}
	stop := startDescriptor(t, a, d)
	require.Eventually(t, func() bool {
		p.mu.Lock()
		defer p.mu.Unlock()
		return p.firstGets > 0
	}, 5*time.Second, 10*time.Millisecond)
	_, err := d.store.Pool().Exec(t.Context(), "DELETE FROM shuttle_symbols WHERE asset_key='first'")
	require.NoError(t, err)
	stop()
	p.mu.Lock()
	p.complete = true
	p.mu.Unlock()
	stop = startDescriptor(t, a, d)
	defer stop()
	require.Eventually(t, func() bool {
		entry, readErr := d.store.PendingSymbol(t.Context(), 1, symbols.Sticker, "second")
		return readErr == nil && entry.Described
	}, 5*time.Second, 10*time.Millisecond)
	state, err := descriptorState(filepath.Join(a.journal, first.String()))
	require.NoError(t, err)
	require.Equal(t, descriptorDonePhase, state.Phase)
	require.NotNil(t, state.Result)
	_, err = os.Stat(filepath.Join(a.journal, first.String(), "blocked.json"))
	require.NoError(t, err)
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Equal(t, 2, p.creates)
	require.Equal(t, 2, p.uploads)
	require.Equal(t, 2, p.chats)
	require.GreaterOrEqual(t, p.firstGets, 2)
}

func TestDescriptorWatchDoesNotRepeatUnknownUploadOrBlockAnotherEntry(t *testing.T) {
	t.Parallel()
	p := &descriptorPeers{loseUpload: true, complete: true}
	d := descriptorDependencies(t, p)
	first := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	seedDescriptor(t, d, "first", first)
	seedDescriptor(t, d, "second", uuid.MustParse("00000000-0000-0000-0000-000000000002"))
	a := arguments{journal: t.TempDir()}
	stop := startDescriptor(t, a, d)
	require.Eventually(t, func() bool {
		entry, err := d.store.PendingSymbol(t.Context(), 1, symbols.Sticker, "second")
		return err == nil && entry.Described
	}, 5*time.Second, 10*time.Millisecond)
	stop()
	state, err := descriptorState(filepath.Join(a.journal, first.String()))
	require.NoError(t, err)
	require.Equal(t, "upload_artifact", state.Pending)
	stop = startDescriptor(t, a, d)
	defer stop()
	require.Never(t, func() bool {
		p.mu.Lock()
		defer p.mu.Unlock()
		return p.uploads != 2 || p.creates != 1
	}, 150*time.Millisecond, 10*time.Millisecond)
}

func TestDescriptorWatchBoundsParallelProceduresAndResumesEachOriginal(t *testing.T) {
	t.Parallel()
	p := &descriptorPeers{}
	d := descriptorDependencies(t, p)
	for _, id := range []string{"one", "two", "three", "four"} {
		seedDescriptor(t, d, id, uuid.New())
	}
	a := arguments{journal: t.TempDir(), concurrency: 2}
	stop := startDescriptor(t, a, d)
	require.Eventually(t, func() bool {
		p.mu.Lock()
		defer p.mu.Unlock()
		return p.creates == 2
	}, 5*time.Second, 10*time.Millisecond)
	require.Never(t, func() bool {
		p.mu.Lock()
		defer p.mu.Unlock()
		return p.creates > 2
	}, 100*time.Millisecond, 10*time.Millisecond)
	stop()
	p.mu.Lock()
	p.complete = true
	p.mu.Unlock()
	stop = startDescriptor(t, a, d)
	defer stop()
	require.Eventually(t, func() bool {
		var remaining int
		err := d.store.Pool().QueryRow(t.Context(),
			"SELECT count(*) FROM shuttle_symbols WHERE description IS NULL").Scan(&remaining)
		return err == nil && remaining == 0
	}, 5*time.Second, 10*time.Millisecond)
	stop()
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Equal(t, 4, p.creates)
	require.Equal(t, 4, p.chats)
	require.Equal(t, 4, p.uploads)
}
