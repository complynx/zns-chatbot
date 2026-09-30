package bot

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/appclient"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/legacyfood"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
	"github.com/complynx/zns-chatbot/platform/internal/passes"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
	"github.com/complynx/zns-chatbot/platform/internal/workflow"
)

// r35Reply is one Core API answer. marked sends the supported database
// provenance header that appclient preserves for every status >= 400. This
// proves the consuming branch for that wire shape; it does not prove that a
// particular Core service currently produces a combined SQL+domain response.
type r35Reply struct {
	body   string
	status int
	marked bool
}

func r35Problem(status int, code string, marked bool) r35Reply {
	return r35Reply{status: status, body: `{"code":"` + code + `"}`, marked: marked}
}

type r35Paths struct {
	mu    sync.Mutex
	paths []string
}

func (p *r35Paths) add(path string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.paths = append(p.paths, path)
}

func (p *r35Paths) list() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Clone(p.paths)
}

// r35API is a real appclient.Client over HTTP. Preferences succeed; every other
// request gets reply, so the first domain call of the handler receives it.
func r35API(t *testing.T, reply r35Reply) (appclient.Client, *r35Paths) {
	t.Helper()
	paths := &r35Paths{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/me/preferences" {
			_, _ = w.Write([]byte(`{"language":"en"}`))
			return
		}
		paths.add(r.URL.Path)
		if reply.marked {
			w.Header().Set(core.DatabaseFailureHeader, "1")
		}
		w.WriteHeader(reply.status)
		_, _ = w.Write([]byte(reply.body))
	}))
	t.Cleanup(server.Close)
	return appclient.Client{
		Base:         server.URL,
		HTTP:         server.Client(),
		SandboxToken: func(owner string) string { return "r35-" + owner },
	}, paths
}

// r35Bot uses a lazily dialed pool whose every dial is refused and counted, so
// a test observes whether a fallback attempted persistence (queue/record).
func r35Bot(t *testing.T, api appclient.Client) (*Bot, *atomic.Int32) {
	t.Helper()
	config, err := pgxpool.ParseConfig("postgres://bot@fake-postgres/bot?sslmode=disable")
	require.NoError(t, err)
	var dials atomic.Int32
	config.ConnConfig.LookupFunc = func(context.Context, string) ([]string, error) {
		return []string{"127.0.0.1"}, nil
	}
	config.ConnConfig.DialFunc = func(context.Context, string, string) (net.Conn, error) {
		dials.Add(1)
		return nil, errors.New("r35 dial refused")
	}
	pool, err := pgxpool.NewWithConfig(context.Background(), config)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return &Bot{DB: pool, API: api}, &dials
}

const r35Owner = "owner-1"

func r35Message(text string) (incoming, telegram.Update) {
	return incoming{owner: r35Owner, chat: 1, text: text, origin: originManual},
		telegram.Update{ID: 7, Message: &telegram.Message{ID: 9, Text: text}}
}

type r35WireHandler struct {
	run   func(context.Context, *Bot) error
	path  string
	reply func(marked bool) r35Reply
}

func r35Forbidden(marked bool) r35Reply {
	return r35Problem(http.StatusForbidden, mediaForbidden, marked)
}

// Actual handlers whose first domain API call feeds the named fallback.
func r35WireHandlers() map[string]r35WireHandler {
	command := func(text string, handle func(*Bot, context.Context, incoming, telegram.Update) error) func(
		context.Context, *Bot,
	) error {
		return func(ctx context.Context, b *Bot) error {
			in, update := r35Message(text)
			return handle(b, ctx, in, update)
		}
	}
	return map[string]r35WireHandler{
		"credits": {
			run: command(creditsUsageCommand, (*Bot).handleCredits), path: "/v1/credits/usage", reply: r35Forbidden,
		},
		"admin utility": {
			run:  command("/user_echo 5", (*Bot).handleAdminUtility),
			path: "/v1/admin-utilities/authorize", reply: r35Forbidden,
		},
		"admin message": {
			run:  command("/send_message_to", (*Bot).handleAdminMessage),
			path: "/v1/admin-messages/capabilities", reply: r35Forbidden,
		},
		"model settings": {
			run:  command(modelSettingsCommandName, (*Bot).handleModelSettings),
			path: "/v1/model-settings/permissions", reply: r35Forbidden,
		},
		"payment inbox": {
			run: func(ctx context.Context, b *Bot) error {
				return b.renderPaymentInbox(ctx, r35Owner, 1, orders.Event{ID: "e1"}, nil,
					map[string]bool{}, map[string]bool{}, "en")
			},
			path: "/v1/order-events/e1/payment-inbox", reply: r35Forbidden,
		},
		"food stale version": {
			run: func(ctx context.Context, b *Bot) error {
				in, _ := r35Message("")
				return b.performFood(ctx, in, 7, legacyfood.Command{Name: "select", EventID: "e1", OrderID: "o1"})
			},
			path: "/v1/food/commands",
			reply: func(marked bool) r35Reply {
				return r35Problem(http.StatusConflict, "stale_version", marked)
			},
		},
	}
}

func TestR35MarkedMatching4xxSkipsCommandFallback(t *testing.T) {
	t.Parallel()
	for name, handler := range r35WireHandlers() {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			api, paths := r35API(t, handler.reply(true))
			b, dials := r35Bot(t, api)
			requireR33NoDomainMarker(t, handler.run(t.Context(), b))
			require.Equal(t, []string{handler.path}, paths.list())
			require.Zero(t, dials.Load(), "no failure notice may be queued or recorded")
		})
	}
}

func TestR35MarkedServerFailureStaysDatabaseFailure(t *testing.T) {
	t.Parallel()
	for name, handler := range r35WireHandlers() {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			api, paths := r35API(t, r35Problem(http.StatusInternalServerError, "internal_error", true))
			b, dials := r35Bot(t, api)
			require.True(t, core.IsDatabaseFailure(handler.run(t.Context(), b)))
			require.Equal(t, []string{handler.path}, paths.list())
			require.Zero(t, dials.Load())
		})
	}
}

// Ordinary controls whose fallback completes without persistence.
func TestR35OrdinaryForbiddenKeepsSilentFallback(t *testing.T) {
	t.Parallel()
	t.Run("payment inbox omits section", func(t *testing.T) {
		t.Parallel()
		api, paths := r35API(t, r35Forbidden(false))
		b, dials := r35Bot(t, api)
		require.NoError(t, r35WireHandlers()["payment inbox"].run(t.Context(), b))
		require.Equal(t, []string{"/v1/order-events/e1/payment-inbox"}, paths.list())
		require.Zero(t, dials.Load())
	})
}

func r35AdminInput(ctx context.Context, b *Bot) (bool, error) {
	in, update := r35Message("reply text")
	update.Message.ReplyToMessage = &telegram.Message{ID: 3}
	return b.handleAdminMessageInput(ctx, in, update)
}

func TestR35AdminInputPendingLookup(t *testing.T) {
	t.Parallel()
	const pending = "/v1/admin-messages/input/pending"
	t.Run("marked 403 is fatal, not unrelated text", func(t *testing.T) {
		t.Parallel()
		api, paths := r35API(t, r35Forbidden(true))
		b, dials := r35Bot(t, api)
		handled, err := r35AdminInput(t.Context(), b)
		requireR33NoDomainMarker(t, err)
		require.False(t, handled)
		require.Equal(t, []string{pending}, paths.list())
		require.Zero(t, dials.Load())
	})
	t.Run("ordinary 403 continues as unrelated text", func(t *testing.T) {
		t.Parallel()
		api, paths := r35API(t, r35Forbidden(false))
		b, dials := r35Bot(t, api)
		handled, err := r35AdminInput(t.Context(), b)
		require.NoError(t, err)
		require.False(t, handled)
		require.Equal(t, []string{pending}, paths.list())
		require.Zero(t, dials.Load())
	})
	t.Run("malformed provider response stays ordinary", func(t *testing.T) {
		t.Parallel()
		api, _ := r35API(t, r35Reply{status: http.StatusOK})
		b, dials := r35Bot(t, api)
		handled, err := r35AdminInput(t.Context(), b)
		require.Error(t, err)
		require.False(t, core.IsDatabaseFailure(err))
		require.False(t, handled)
		require.Zero(t, dials.Load())
	})
}

// Local error shapes entering the actual mappers: DatabaseFailure(ProblemError)
// as produced by appclient, marker joined with cancellation, and plain safe SQL.
func r35PositiveErrors() map[string]error {
	return map[string]error{
		"marked forbidden": core.DatabaseFailure(
			&core.ProblemError{Status: http.StatusForbidden, Code: mediaForbidden},
		),
		"wrapped marked not found": fmt.Errorf("api: %w", core.DatabaseFailure(
			&core.ProblemError{Status: http.StatusNotFound, Code: "not_found"},
		)),
		"marker joined with cancellation": errors.Join(
			core.DatabaseFailure(&core.ProblemError{Status: http.StatusConflict, Code: "stale_version"}),
			context.Canceled,
		),
		"plain safe sql": core.ErrDatabase,
	}
}

type r35Mapper func(context.Context, *Bot, error) error

func r35Mappers() map[string]r35Mapper {
	return map[string]r35Mapper{
		"workflow outcome": func(ctx context.Context, b *Bot, err error) error {
			_, out := b.workflowOutcome(ctx, r35Owner, 7,
				workflow.Action{Name: "book", Origin: originManual}, workflow.Workflow{}, err)
			return out
		},
		"profile outcome": func(ctx context.Context, b *Bot, err error) error {
			in, _ := r35Message("")
			_, out := b.profileOutcome(ctx, in, 7, passes.Command{Name: "submit"}, passes.Profile{}, err, "en")
			return out
		},
		"proof failure": func(ctx context.Context, b *Bot, err error) error {
			_, out := b.proofFailure(ctx, r35Owner, err)
			return out
		},
	}
}

func TestR35MapperPositiveSQLWinsBeforeRefusal(t *testing.T) {
	t.Parallel()
	for mapperName, mapper := range r35Mappers() {
		for errName, positive := range r35PositiveErrors() {
			t.Run(mapperName+"/"+errName, func(t *testing.T) {
				t.Parallel()
				b, dials := r35Bot(t, appclient.Client{})
				requireR33NoDomainMarker(t, mapper(t.Context(), b, positive))
				require.Zero(t, dials.Load(), "no refusal may be recorded")
			})
		}
	}
}

func TestR35MapperOrdinaryFailuresKeepCause(t *testing.T) {
	t.Parallel()
	for mapperName, mapper := range r35Mappers() {
		for index, ordinary := range []error{
			&core.ProblemError{Status: http.StatusInternalServerError, Code: "internal_error"},
			io.EOF,
			context.Canceled,
		} {
			t.Run(mapperName+"/"+strconv.Itoa(index), func(t *testing.T) {
				t.Parallel()
				b, dials := r35Bot(t, appclient.Client{})
				out := mapper(t.Context(), b, ordinary)
				require.Equal(t, ordinary, out)
				require.False(t, core.IsDatabaseFailure(out))
				require.Zero(t, dials.Load())
			})
		}
	}
}
