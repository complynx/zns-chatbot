package integration_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/bot"
	"github.com/complynx/zns-chatbot/platform/internal/conversation"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
	knowledgeauthority "github.com/complynx/zns-chatbot/platform/internal/knowledge/authority"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

func TestKnowledgeOwnedMemoryDeletionAfterExposure(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"memo", "document"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			f := setup(t)
			service := knowledge.Service{DB: f.db}
			command := knowledge.Command{
				Name:    knowledge.MemoSet,
				Key:     "seed",
				FactKey: "summary.overview",
				Text:    "private memory canary",
			}
			if kind == "document" {
				command.Name = knowledge.DocumentSet
				command.Topic = "summary"
				command.FactKey = "overview"
			}
			_, err := service.Execute(t.Context(), "alice", command)
			require.NoError(t, err)
			f.b.Model = avModel(func(ctx context.Context, input agent.Input) (agent.Plan, error) {
				encoded, encodeErr := json.Marshal(input.Knowledge)
				require.NoError(t, encodeErr)
				require.Contains(t, string(encoded), "private memory canary")
				command.Name = knowledge.MemoDelete
				if kind == "document" {
					command.Name = knowledge.DocumentDelete
				}
				command.Key = "delete"
				command.Text = ""
				command.Version = 1
				_, deleteErr := service.Execute(ctx, "alice", command)
				require.NoError(t, deleteErr)
				return agent.Plan{View: "workflow", Text: "derived private memory canary"}, nil
			})
			require.ErrorContains(
				t,
				f.b.Handle(t.Context(), message(7301, identity.AliceTelegramID, "Read memory")),
				"terminal",
			)
		})
	}
}

func TestKnowledgeMemorySourcesRechecksDerivedAuthority(t *testing.T) {
	t.Parallel()
	f := knowledgeAuthorizationFixture(t)
	ctx := t.Context()
	history := conversation.Service{DB: f.db}
	refs := []readsource.Authority{{Knowledge: knowledgeauthority.ReadAuthority{Kind: knowledgeauthority.Review}}}
	require.NoError(t, history.AppendDerived(ctx, "bob", "source-private", "derived review canary", 0, refs))
	service := knowledge.Service{DB: f.db}
	command := knowledge.Command{
		Name:    knowledge.DocumentSet,
		Key:     "source-doc",
		Topic:   "travel",
		FactKey: "train",
		Text:    "source reference",
	}
	_, err := service.ExecuteWithSources(ctx, "bob", command, []string{"source-private"})
	require.NoError(t, err)
	page, err := service.SearchMemory(
		ctx,
		"bob",
		knowledge.MemoryQuery{Namespace: knowledge.MemoryPrivate, Topic: "travel"},
	)
	require.NoError(t, err)
	require.Len(t, page.Entries, 1)
	before, err := service.MemorySources(ctx, "bob", page.Entries[0].Ref)
	require.NoError(t, err)
	require.Len(t, before.Events, 1)
	require.Equal(t, "derived review canary", before.Events[0].Text)
	_, err = f.db.Exec(ctx, `DELETE FROM core.knowledge_permissions WHERE actor='bob'`)
	require.NoError(t, err)
	after, err := service.MemorySources(ctx, "bob", page.Entries[0].Ref)
	require.NoError(t, err)
	encoded, err := json.Marshal(after)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "derived review canary")
}

func TestKnowledgeSharedMemoryDeletionByDifferentCurator(t *testing.T) {
	t.Parallel()
	f := knowledgeAuthorizationFixture(t)
	service := knowledge.Service{DB: f.db}
	command := knowledge.Command{
		Name:    knowledge.Curate,
		Key:     "seed-shared",
		Topic:   "summary",
		FactKey: "overview",
		Text:    "shared memory canary",
	}
	_, err := service.Execute(t.Context(), "bob", command)
	require.NoError(t, err)
	f.b.Model = avModel(func(ctx context.Context, input agent.Input) (agent.Plan, error) {
		encoded, encodeErr := json.Marshal(input.Knowledge)
		require.NoError(t, encodeErr)
		require.Contains(t, string(encoded), "shared memory canary")
		command.Name = knowledge.RemoveFact
		command.Key = "delete-shared"
		command.Text = ""
		command.Version = 1
		_, deleteErr := service.Execute(ctx, "bob", command)
		require.NoError(t, deleteErr)
		return agent.Plan{View: "workflow", Text: "derived shared memory canary"}, nil
	})
	require.ErrorContains(
		t,
		f.b.Handle(t.Context(), message(7302, identity.AliceTelegramID, "Read shared memory")),
		"terminal",
	)
}

func TestKnowledgeDerivedHistoryAndSummaryRevocation(t *testing.T) {
	t.Parallel()
	f := knowledgeAuthorizationFixture(t)
	s := conversation.Service{DB: f.db}
	ctx := t.Context()
	refs := []readsource.Authority{{Knowledge: knowledgeauthority.ReadAuthority{Kind: knowledgeauthority.Review}}}
	body := strings.Repeat("authorized knowledge body ", 300)
	require.NoError(t, s.AppendOriginal(ctx, "bob", "original", "assistant", body))
	require.NoError(t, s.AppendDerived(ctx, "bob", "long", body, 0, refs))
	require.NoError(t, s.AppendDerived(ctx, "bob", "short", "knowledge summary source", 0, refs))
	page, err := s.Read(ctx, "bob", conversation.Query{Limit: 3})
	require.NoError(t, err)
	require.Equal(t, refs, page.Events[0].ReadAuthorities)
	longID, originalID := page.Events[1].ID, page.Events[2].ID
	chunk, err := s.ReadText(ctx, "bob", longID, 0, conversation.MaxChunkCharacters, "")
	require.NoError(t, err)
	require.Contains(t, chunk.Text, "authorized knowledge body")
	require.NoError(t, s.CommitSummary(ctx, "bob", 0, []int64{page.Events[0].ID}, "private summarized knowledge"))
	window, err := s.Window(ctx, "bob", 1)
	require.NoError(t, err)
	require.Equal(t, refs, window.Summary.ReadAuthorities)
	require.NoError(
		t,
		s.AppendDerived(ctx, "bob", "inherited", "private inherited knowledge", 0, window.Summary.ReadAuthorities),
	)
	require.NoError(t, s.AppendOriginal(ctx, "bob", "recent", "user", "unrelated original"))
	_, err = f.db.Exec(ctx, `DELETE FROM core.knowledge_permissions WHERE actor='bob'`)
	require.NoError(t, err)
	window, err = s.Window(ctx, "bob", 1)
	require.NoError(t, err)
	require.Empty(t, window.Summary.Text)
	require.Positive(t, window.Generation)
	page, err = s.Read(ctx, "bob", conversation.Query{Limit: 5})
	require.NoError(t, err)
	for _, event := range page.Events {
		if event.ID != originalID && event.Text != "unrelated original" {
			require.True(t, event.Omitted)
		}
	}
	chunk, err = s.ReadText(ctx, "bob", longID, 0, 100, "")
	require.NoError(t, err)
	require.Empty(t, chunk.Text)
	original, err := s.ReadText(ctx, "bob", originalID, 0, 100, "")
	require.NoError(t, err)
	require.Contains(t, original.Text, "authorized knowledge body")
	requireCode(t, s.AppendDerived(ctx, "bob", "late", "late private reply", window.Generation, refs), "history_stale")
}

func TestKnowledgeSavedAnswerRevocation(t *testing.T) {
	t.Parallel()
	f := knowledgeAuthorizationFixture(t)
	reads := []agent.KnowledgeReadResult{
		{
			Request:   agent.KnowledgeProposal{Name: agent.KnowledgeProposals, ReviewQueue: true},
			Proposals: []knowledge.Proposal{{Text: "private reviewed body"}},
		},
	}
	_, err := f.db.Exec(
		t.Context(),
		`INSERT INTO bot.interactions(owner,update_id,kind,content) VALUES('bob',7330,'knowledge_reads',$1)`,
		reads,
	)
	require.NoError(t, err)
	calls := 0
	model := avModel(func(_ context.Context, input agent.Input) (agent.Plan, error) {
		calls++
		require.NotEmpty(t, input.Knowledge.Reads[0].Proposals)
		return agent.Plan{View: "workflow", Text: "saved private review answer"}, nil
	})
	f.b.Model = model
	update := message(7330, identity.BobTelegramID, "Read private review")
	handle(t, f.b, update)
	require.Equal(t, 1, calls)
	_, err = f.db.Exec(t.Context(), `DELETE FROM core.knowledge_permissions WHERE actor='bob'`)
	require.NoError(t, err)
	f.b = &bot.Bot{DB: f.db, API: f.b.API, Host: f.b.Host, TG: f.b.TG, Model: model}
	require.ErrorContains(t, f.b.Handle(t.Context(), update), "terminal")
	require.Equal(t, 1, calls)
	require.NoError(t, f.b.Render(t.Context(), "bob", identity.BobTelegramID))
	cards, err := json.Marshal(chatMessages(t, f, identity.BobTelegramID))
	require.NoError(t, err)
	require.NotContains(t, string(cards), "saved private review answer")
	page, err := (conversation.Service{DB: f.db}).Read(t.Context(), "bob", conversation.Query{Limit: 10})
	require.NoError(t, err)
	encoded, err := json.Marshal(page)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "saved private review answer")
}

func TestKnowledgeSharedEpochZeroDeletionLock(t *testing.T) {
	t.Parallel()
	service := knowledgeFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	command := knowledge.Command{
		Name:    knowledge.Curate,
		Event:   "kb-current",
		Key:     "seed",
		Topic:   "summary",
		FactKey: "overview",
		Text:    "shared epoch zero",
	}
	_, err := service.Execute(ctx, "kbadmin", command)
	require.NoError(t, err)
	tx, err := service.DB.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	// Intentionally list the target review grant before shared memory. The checker
	// must acquire the common global gate before checking this target leaf.
	refs := []readsource.Authority{
		{Knowledge: knowledgeauthority.ReadAuthority{Kind: knowledgeauthority.Review, Scope: "kb-current"}},
		{Knowledge: knowledgeauthority.ReadAuthority{Kind: knowledgeauthority.SharedMemory}},
	}
	allowed, err := readsource.Lock(ctx, tx, "bob", refs)
	require.NoError(t, err)
	require.Equal(t, []bool{true, true}, allowed)
	command.Name = knowledge.RemoveFact
	command.Key = "delete"
	command.Text = ""
	command.Version = 1
	done := make(chan error, 1)
	go func() { _, deleteErr := service.Execute(ctx, "kbadmin", command); done <- deleteErr }()
	require.Eventually(t, func() bool {
		var waiting bool
		err = service.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE '%knowledge_scopes%')`).
			Scan(&waiting)
		return err == nil && waiting
	}, time.Second, 10*time.Millisecond)
	select {
	case err = <-done:
		t.Fatalf("deletion crossed source lock: %v", err)
	default:
	}
	require.NoError(t, tx.Commit(ctx))
	require.NoError(t, <-done)
	history := conversation.Service{DB: service.DB}
	requireCode(t, history.AppendDerived(ctx, "bob", "late-shared", "stale shared answer", 0, refs), "history_stale")
}

func TestKnowledgeScriptSourcesFenceLateReply(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"review", "private_document"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			f := knowledgeAuthorizationFixture(t)
			service := knowledge.Service{DB: f.db}
			code := `return tools.knowledge.review_queue({}).items[0].text;`
			canary := "Private pending proposal"
			if scenario == "private_document" {
				_, err := service.Execute(
					t.Context(),
					"bob",
					knowledge.Command{
						Name:    knowledge.DocumentSet,
						Key:     "seed",
						Topic:   "travel",
						FactKey: "hidden",
						Text:    "tool-only private document",
					},
				)
				require.NoError(t, err)
				code = `return tools.memory.search({namespace:"private",topic:"travel"}).entries[0].text;`
				canary = "tool-only private document"
			}
			f.b.Scripts = scopeVM{}
			calls := 0
			f.b.Model = avModel(func(ctx context.Context, input agent.Input) (agent.Plan, error) {
				calls++
				if calls == 1 {
					return agent.Plan{
						View:         "workflow",
						ScriptAction: &agent.ScriptProposal{Code: code, InputJSON: "null"},
					}, nil
				}
				require.Len(t, input.Script.Runs, 1)
				require.Contains(t, string(input.Script.Runs[0].Result), canary)
				if scenario == "review" {
					_, err := f.db.Exec(ctx, `DELETE FROM core.knowledge_permissions WHERE actor='bob'`)
					require.NoError(t, err)
				} else {
					_, err := service.Execute(
						ctx,
						"bob",
						knowledge.Command{
							Name:    knowledge.DocumentDelete,
							Key:     "delete",
							Topic:   "travel",
							FactKey: "hidden",
							Version: 1,
						},
					)
					require.NoError(t, err)
				}
				return agent.Plan{View: "workflow", Text: "late tool-derived answer"}, nil
			})
			require.ErrorContains(
				t,
				f.b.Handle(t.Context(), message(7390, identity.BobTelegramID, "Read the selected private source")),
				"terminal",
			)
			require.Equal(t, 2, calls)
			var count int
			require.NoError(
				t,
				f.db.QueryRow(t.Context(), `SELECT count(*) FROM core.conversation_events WHERE owner='bob' AND source_key='tg-assistant-7390'`).
					Scan(&count),
			)
			require.Zero(t, count)
		})
	}
}
