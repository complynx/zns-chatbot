package integration_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf16"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/loom"
	"github.com/complynx/zns-chatbot/internal/telegram"
)

func (p *peers) externalApprovalDecision(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Choice string `json:"choice"`
	}
	if r.Method != http.MethodPost || r.Header.Get("X-Subject-Token") == "" ||
		r.Header.Get("Authorization") == "" || json.NewDecoder(r.Body).Decode(&in) != nil {
		http.Error(w, "invalid decision", http.StatusBadRequest)
		return
	}
	p.mcpDecisions = append(p.mcpDecisions, in.Choice)
	respond(w, map[string]string{"status": "approved"})
}

func TestExternalAgentApprovalUsesTelegramWithoutAgentRun(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.mcpWriteSecret = "fixture-mcp-delivery-secret"
	h := setup(t, p)
	owner := scopedAccount(t, h, 123, 456, "external-owner")
	other := scopedAccount(t, h, 123, 457, "other-owner")
	approval := loom.ExternalApproval{
		ID: uuid.NewString(), Subject: owner.Subject, OrgID: owner.Organization, Client: "fixture-client",
		Service: "zinusya", Module: "zinusya_knowledge", Method: "suggest", Description: "Suggest the displayed change.",
		Arguments: json.RawMessage(`{"title":"Proposal","body":"Proposed change"}`), Remember: true,
		ExpiresAt: time.Now().Add(time.Minute),
	}
	require.Equal(t, http.StatusUnauthorized, notifyExternalApproval(t, h, approval, "wrong"))
	// A previous worker committed its claim and stopped before sending.
	id := uuid.MustParse(approval.ID)
	nextPart, claimed, err := h.store.ClaimExternalApproval(
		t.Context(), id, owner.ID, uuid.New(), approval.ExpiresAt, make([]byte, sha256.Size),
	)
	require.NoError(t, err)
	require.True(t, claimed)
	require.Zero(t, nextPart)
	require.Equal(t, http.StatusServiceUnavailable, notifyExternalApproval(t, h, approval, p.mcpWriteSecret))
	_, err = h.store.Pool().Exec(t.Context(), `UPDATE shuttle_external_approval_messages
		SET lease_until=clock_timestamp()-interval '1 second' WHERE operation_id=$1`, id)
	require.NoError(t, err)
	for range 2 {
		require.Equal(t, http.StatusOK, notifyExternalApproval(t, h, approval, p.mcpWriteSecret))
	}
	h.awaitReplies(t, 1)
	p.mu.Lock()
	markup, ok := p.sent[0].ReplyMarkup.(*telegram.InlineKeyboardMarkup)
	p.mu.Unlock()
	require.True(t, ok)
	require.Len(t, markup.InlineKeyboard, 4)
	choice := markup.InlineKeyboard[3][0].CallbackData
	communicationClick(t, knowledgeFixture{h: h}, other.TelegramUserID, choice, 458)
	communicationClick(t, knowledgeFixture{h: h}, owner.TelegramUserID, choice, 459)
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Empty(t, p.runs)
	require.Equal(t, []string{"all"}, p.mcpDecisions)
}

func notifyExternalApproval(t *testing.T, h *harness, approval loom.ExternalApproval, secret string) int {
	t.Helper()
	return postExternalApproval(h, externalApprovalRequest(t, approval, secret))
}

func externalApprovalRequest(t *testing.T, approval loom.ExternalApproval, secret string) *http.Request {
	t.Helper()
	raw, err := json.Marshal(approval)
	require.NoError(t, err)
	request := httptest.NewRequest(http.MethodPost, "/internal/agent/external-agent/approval", bytes.NewReader(raw))
	request.Header.Set("Authorization", "Bearer "+secret)
	return request
}

func postExternalApproval(h *harness, request *http.Request) int {
	out := httptest.NewRecorder()
	h.endpoint.InternalHandler().ServeHTTP(out, request)
	return out.Code
}

func TestExternalAgentOrderApprovalRequiresFullReceiptBeforeButtons(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.mcpWriteSecret = "fixture-mcp-delivery-secret"
	id := uuid.New()
	otherID := uuid.New()
	lastEntered, allowLast := make(chan struct{}), make(chan struct{})
	p.beforeSend = func(message telegram.SendMessageRequest) {
		if message.ReplyMarkup != nil && strings.HasPrefix(message.Text, "MCP "+id.String()+" ") {
			close(lastEntered)
			select {
			case <-allowLast:
			case <-t.Context().Done():
			}
		}
	}
	f, scope, args := orderApprovalFixture(t, p, true)
	prepared, err := f.H.store.PrepareOwnOrderSubmission(t.Context(), scope, id, args, db.OrderSourceAgent)
	require.NoError(t, err)
	raw, err := json.Marshal(args)
	require.NoError(t, err)
	a := loom.ExternalApproval{
		ID: id.String(), Subject: scope.Subject, OrgID: scope.Organization, Client: "fixture-client",
		Service: "zinusya", Module: "zinusya_orders", Method: "submit", Description: "INCOMPLETE SUMMARY",
		ExpiresAt: prepared.ExpiresAt,
	}
	mismatched := args
	mismatched.ExpectedDraftVersion++
	a.Arguments, err = json.Marshal(mismatched)
	require.NoError(t, err)
	require.Equal(t, http.StatusServiceUnavailable, notifyExternalApproval(t, f.H, a, p.mcpWriteSecret))
	f.H.awaitReplies(t, 0)

	a.Arguments = raw
	otherPrepared, err := f.H.store.PrepareOwnOrderSubmission(t.Context(), scope, otherID, args, db.OrderSourceAgent)
	require.NoError(t, err)
	other := a
	other.ID, other.ExpiresAt = otherID.String(), otherPrepared.ExpiresAt
	delivered := make(chan int, 1)
	request := externalApprovalRequest(t, a, p.mcpWriteSecret)
	go func() { delivered <- postExternalApproval(f.H, request) }()
	func() {
		defer close(allowLast)
		select {
		case <-lastEntered:
		case <-time.After(5 * time.Second):
			t.Fatal("first request did not reach its final chunk")
		}
		require.Equal(t, http.StatusOK, notifyExternalApproval(t, f.H, other, p.mcpWriteSecret))
	}()
	require.Equal(t, http.StatusOK, <-delivered)
	p.mu.Lock()
	defer p.mu.Unlock()
	require.GreaterOrEqual(t, len(p.sent), 4)
	require.Contains(t, p.sent[0].Text, "MCP "+id.String())
	require.Contains(t, p.sent[len(p.sent)-2].Text, "MCP "+otherID.String())
	require.Contains(t, p.sent[len(p.sent)-1].Text, "MCP "+id.String())
	verifyExternalOrderApprovalParts(t, p.sent, id)
	verifyExternalOrderApprovalParts(t, p.sent, otherID)
	require.Empty(t, p.runs)
}

func verifyExternalOrderApprovalParts(t *testing.T, sent []telegram.SendMessageRequest, id uuid.UUID) {
	t.Helper()
	var parts []telegram.SendMessageRequest
	for _, message := range sent {
		if strings.HasPrefix(message.Text, "MCP "+id.String()+" ") {
			parts = append(parts, message)
		}
	}
	require.Greater(t, len(parts), 1)
	var text strings.Builder
	var presentation string
	for n, message := range parts {
		require.LessOrEqual(t, len(utf16.Encode([]rune(message.Text))), 4096)
		header, body, _ := strings.Cut(message.Text, "\n")
		ref, ok := strings.CutPrefix(header, fmt.Sprintf("MCP %s [%d/%d] ", id, n+1, len(parts)))
		require.True(t, ok, header)
		require.Len(t, ref, 8)
		if n == 0 {
			presentation = ref
		}
		require.Equal(t, presentation, ref)
		text.WriteString(body)
		if n < len(parts)-1 {
			require.Nil(t, message.ReplyMarkup)
		}
	}
	for n := range 40 {
		require.Contains(t, text.String(), fmt.Sprintf("lunch/item_%02d", n))
	}
	require.Contains(t, text.String(), "988.20 BYN")
	require.NotContains(t, text.String(), "INCOMPLETE SUMMARY")
	keyboard, ok := parts[len(parts)-1].ReplyMarkup.(*telegram.InlineKeyboardMarkup)
	require.True(t, ok)
	require.Len(t, keyboard.InlineKeyboard, 2)
	require.Equal(t, "ma:"+id.String()+":once", keyboard.InlineKeyboard[0][0].CallbackData)
}

func TestExternalAgentApprovalResumesAfterTelegramRateLimit(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.mcpWriteSecret = "fixture-mcp-delivery-secret"
	id := uuid.New()
	second := "MCP " + id.String() + " [2/"
	rejectedOnce := false
	p.beforeSend = func(message telegram.SendMessageRequest) {
		if !strings.HasPrefix(message.Text, second) {
			return
		}
		p.mu.Lock()
		defer p.mu.Unlock()
		if !rejectedOnce {
			p.rejectSendOnce = true
			rejectedOnce = true
		}
	}
	h := setup(t, p)
	owner := scopedAccount(t, h, 123, 456, "external-owner")
	a := loom.ExternalApproval{
		ID: id.String(), Subject: owner.Subject, OrgID: owner.Organization, Client: "fixture-client",
		Service: "zinusya", Module: "zinusya_knowledge", Method: "suggest", Description: "Suggest this change.",
		ExpiresAt: time.Now().Add(time.Minute),
	}
	var err error
	a.Arguments, err = json.Marshal(map[string]string{"body": strings.Repeat("Proposed detail. ", 700)})
	require.NoError(t, err)
	require.Equal(t, http.StatusServiceUnavailable, notifyExternalApproval(t, h, a, p.mcpWriteSecret))
	var nextPart int
	var hasRetryDelay bool
	require.NoError(t, h.store.Pool().QueryRow(t.Context(), `SELECT next_part, lease_until>clock_timestamp()
		FROM shuttle_external_approval_messages WHERE operation_id=$1`, id).Scan(&nextPart, &hasRetryDelay))
	require.Equal(t, 1, nextPart)
	require.True(t, hasRetryDelay)
	allowed, err := h.store.CanAnswerExternalApproval(t.Context(), id, owner.ID, 1)
	require.NoError(t, err)
	require.False(t, allowed, "The first part cannot authorize the action")
	require.Equal(t, http.StatusServiceUnavailable, notifyExternalApproval(t, h, a, p.mcpWriteSecret))
	h.awaitReplies(t, 2)

	_, err = h.store.Pool().Exec(t.Context(), `UPDATE shuttle_external_approval_messages
		SET lease_until=clock_timestamp()-interval '1 second' WHERE operation_id=$1`, id)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, notifyExternalApproval(t, h, a, p.mcpWriteSecret))
	p.mu.Lock()
	defer p.mu.Unlock()
	require.GreaterOrEqual(t, len(p.sent), 4)
	require.Equal(t, p.sent[1].Text, p.sent[2].Text, "Only the rejected part can be retried")
	for _, message := range p.sent[1:] {
		require.NotContains(t, message.Text, "MCP "+id.String()+" [1/")
	}
	for _, message := range p.sent[:len(p.sent)-1] {
		require.Nil(t, message.ReplyMarkup)
	}
	require.NotNil(t, p.sent[len(p.sent)-1].ReplyMarkup)
	allowed, err = h.store.CanAnswerExternalApproval(t.Context(), id, owner.ID, 1)
	require.NoError(t, err)
	require.False(t, allowed, "A different message remains invalid after final delivery")
	allowed, err = h.store.CanAnswerExternalApproval(t.Context(), id, owner.ID, int64(len(p.sent)))
	require.NoError(t, err)
	require.True(t, allowed, "Only the recorded final message can authorize the action")
}

func TestExternalAgentCatalogApprovalShowsExactReplacementPrices(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.mcpWriteSecret = "fixture-mcp-delivery-secret"
	f := setupPayment(t, p)
	admin, err := f.H.store.GetUser(t.Context(), f.AdminID)
	require.NoError(t, err)
	a := db.OrdersCatalogChange{
		EventID: f.Event.ID, ExpectedEventVersion: f.Event.Version, Document: catalogDocument(),
	}
	previousMessages := 0
	for _, price := range []int64{1234, 4321} {
		a.Document.Items[0].BasePriceMinor = price
		id := uuid.New()
		prepared := prepareCatalog(t, f, id, a)
		args, marshalErr := json.Marshal(prepared.Arguments)
		require.NoError(t, marshalErr)
		approval := loom.ExternalApproval{
			ID: id.String(), Subject: admin.Subject, OrgID: admin.Organization, Client: "fixture-client",
			Service: "zinusya", Module: "zinusya_administration", Method: "set_orders_catalog",
			Description: "Replace catalog with 1 item and 1 offer.", Arguments: args, ExpiresAt: prepared.ExpiresAt,
		}
		require.Equal(t, http.StatusOK, notifyExternalApproval(t, f.H, approval, p.mcpWriteSecret))
		p.mu.Lock()
		newMessages := append([]telegram.SendMessageRequest(nil), p.sent[previousMessages:]...)
		previousMessages = len(p.sent)
		p.mu.Unlock()
		require.NotEmpty(t, newMessages)
		var terms strings.Builder
		for n, message := range newMessages {
			terms.WriteString(message.Text)
			if n < len(newMessages)-1 {
				require.Nil(t, message.ReplyMarkup)
			}
		}
		require.Contains(t, terms.String(), "Soup")
		require.Contains(t, terms.String(), strconv.FormatInt(price, 10))
		require.NotNil(t, newMessages[len(newMessages)-1].ReplyMarkup)
	}
}
