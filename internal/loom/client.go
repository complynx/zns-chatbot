// Package loom implements Loom's versioned service delegation protocol.
package loom

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const ReplyModeAgent = "agent"
const ReplyModeProtocol = "protocol"

const MaxResponseBytes = 1 << 20
const defaultHTTPTimeout = 10 * time.Second

type Auth struct {
	ServiceToken string
	SubjectToken string
	SessionID    string
}
type Session struct {
	ID        string    `json:"auth_session_id"`
	Version   int64     `json:"version"`
	ExpiresAt time.Time `json:"expires_at"`
}
type Input struct {
	Text        string   `json:"text"`
	ArtifactIDs []string `json:"artifact_ids,omitempty"`
}
type Interaction struct {
	ReplyMode string `json:"reply_mode"`
	Questions string `json:"questions"`
	Approvals bool   `json:"approvals"`
}
type Delivery struct {
	Mode   string `json:"mode"`
	WaitMS int    `json:"wait_ms"`
}
type Budget struct {
	MaxMicrocredits int64 `json:"max_microcredits"`
}

type Discovery struct {
	ExpandServices []string `json:"expand_services,omitempty"`
}

// CreateRunRequest contains semantic arguments; the session is sent in an authorization header.
type CreateRunRequest struct {
	ChatID           string      `json:"chat_id,omitempty"`
	PreviousRunID    string      `json:"previous_run_id,omitempty"`
	Input            Input       `json:"input"`
	Interaction      Interaction `json:"interaction"`
	Discovery        *Discovery  `json:"discovery,omitempty"`
	DisconnectPolicy string      `json:"disconnect_policy"`
	Delivery         Delivery    `json:"delivery"`
	Budget           Budget      `json:"budget"`
}
type Choice struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}
type Question struct {
	Answer    *InputRequest `json:"answer,omitempty"`
	ID        string        `json:"question_id"`
	Text      string        `json:"text"`
	Choices   []Choice      `json:"choices,omitempty"`
	AllowText bool          `json:"allow_text"`
	ExpiresAt time.Time     `json:"expires_at"`
}
type Approval struct {
	BackendOperationID string          `json:"backend_operation_id,omitempty"`
	ReplyMode          string          `json:"reply_mode"`
	ConfirmationText   string          `json:"confirmation_text,omitempty"`
	Decision           string          `json:"decision,omitempty"`
	ID                 string          `json:"approval_id"`
	Text               string          `json:"text"`
	OperationID        string          `json:"operation_id"`
	Method             string          `json:"method"`
	Arguments          json.RawMessage `json:"arguments"`
	ArgumentHash       string          `json:"argument_hash"`
	ConfirmationID     string          `json:"confirmation_id,omitempty"`
	Precondition       string          `json:"precondition,omitempty"`
	ExpiresAt          time.Time       `json:"expires_at"`
}
type Run struct {
	Interaction          Interaction `json:"interaction"`
	ConversationRevision int64       `json:"conversation_revision,omitempty"`
	ID                   string      `json:"run_id"`
	ChatID               string      `json:"chat_id"`
	AuthSessionID        string      `json:"auth_session_id"`
	Status               string      `json:"status"`
	Reason               string      `json:"reason,omitempty"`
	Result               string      `json:"result,omitempty"`
	Version              int64       `json:"version"`
	Question             *Question   `json:"question,omitempty"`
	Approval             *Approval   `json:"approval,omitempty"`
}
type InputPresentation struct {
	ReplyTo    string `json:"reply_to,omitempty"`
	QuestionID string `json:"question_id,omitempty"`
	ApprovalID string `json:"approval_id,omitempty"`
}

type InputContext struct {
	Revision    int64  `json:"revision"`
	SourceActor string `json:"source_actor"`
	QuestionID  string `json:"question_id,omitempty"`
	ApprovalID  string `json:"approval_id,omitempty"`
}

type InputRequest struct {
	Presentation *InputPresentation `json:"presentation,omitempty"`
	Kind         string             `json:"kind"`
	Text         string             `json:"text,omitempty"`
	QuestionID   string             `json:"question_id,omitempty"`
	ChoiceIDs    []string           `json:"choice_ids,omitempty"`
	ApprovalID   string             `json:"approval_id,omitempty"`
	Decision     string             `json:"decision,omitempty"`
}
type ControlReceipt struct {
	InputID string        `json:"input_id,omitempty"`
	Context *InputContext `json:"context,omitempty"`
	ID      string        `json:"run_id"`
	Status  string        `json:"status"`
	Reason  string        `json:"reason,omitempty"`
	Version int64         `json:"version"`
}
type Outcome string

const (
	NotStarted Outcome = "not_started"
	Rejected   Outcome = "rejected"
	Unknown    Outcome = "unknown"
)

const (
	CodeRunTerminal          = "run_terminal"
	CodeUnsupportedReplyMode = "unsupported_reply_mode"
)

type Error struct {
	Code       string
	StatusCode int
	Retryable  bool
	Outcome    Outcome
}

func (e *Error) Error() string {
	return "Loom request failed (" + string(e.Outcome) + ", status " + strconv.Itoa(e.StatusCode) + ")"
}

type Client struct {
	base string
	http *http.Client
}

func New(baseURL string, client *http.Client) (*Client, error) {
	u, err := url.Parse(baseURL)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.RawQuery != "" ||
		u.Fragment != "" {
		return nil, errors.New("invalid Loom base URL")
	}
	h := http.Client{Timeout: defaultHTTPTimeout}
	if client != nil {
		h = *client
	}
	if h.Timeout <= 0 {
		h.Timeout = defaultHTTPTimeout
	}
	h.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Client{base: strings.TrimRight(baseURL, "/"), http: &h}, nil
}
func (c *Client) Bootstrap(ctx context.Context, auth Auth) (Session, error) {
	var result Session
	if auth.SubjectToken == "" || auth.SessionID != "" {
		return result, &Error{Outcome: NotStarted}
	}
	err := c.call(ctx, http.MethodPost, "/auth/sessions", auth, "", struct{}{}, &result, true, false)
	if err == nil && (result.ID == "" || result.Version <= 0 || result.ExpiresAt.IsZero()) {
		err = &Error{Outcome: Unknown}
	}
	return result, err
}
func (c *Client) Reauthorize(ctx context.Context, auth Auth, id string, version int64) (Session, error) {
	var result Session
	if auth.SubjectToken == "" || auth.SessionID != "" || version <= 0 {
		return result, &Error{Outcome: NotStarted}
	}
	path, err := resourcePath("auth/sessions", id, "/reauthorize")
	if err != nil {
		return result, err
	}
	err = c.call(ctx, http.MethodPost, path, auth, "", struct {
		Version int64 `json:"version"`
	}{version}, &result, true, false)
	if err == nil && (result.ID != id || result.Version <= version || result.ExpiresAt.IsZero()) {
		err = &Error{Outcome: Unknown}
	}
	return result, err
}
func (c *Client) CreateRun(ctx context.Context, auth Auth, key string, input CreateRunRequest) (Run, error) {
	var result Run
	if input.DisconnectPolicy == "" {
		input.DisconnectPolicy = "continue"
	}
	if input.Delivery.Mode == "" {
		input.Delivery.Mode = "async"
	}
	if input.Interaction.Questions == "" {
		input.Interaction.Questions = "choices_and_text"
	}
	err := c.call(ctx, http.MethodPost, "/runs", auth, key, input, &result, true, true)
	if err == nil && input.Interaction.ReplyMode == "agent" && result.Interaction.ReplyMode != "agent" {
		return result, &Error{Code: CodeUnsupportedReplyMode, Outcome: Unknown, Retryable: true}
	}
	if err == nil && (result.ID == "" || result.Status == "") {
		err = &Error{Outcome: Unknown, Retryable: true}
	}
	return result, err
}
func (c *Client) GetRun(ctx context.Context, auth Auth, id string) (Run, error) {
	var result Run
	path, err := resourcePath("runs", id, "")
	if err != nil {
		return result, err
	}
	err = c.call(ctx, http.MethodGet, path, auth, "", nil, &result, false, false)
	if err == nil && (result.ID != id || result.Status == "") {
		err = &Error{Outcome: Rejected, Retryable: true}
	}
	return result, err
}
func (c *Client) Input(ctx context.Context, auth Auth, id, key string, input InputRequest) (ControlReceipt, error) {
	var result ControlReceipt
	if input.Kind != "steering" && input.Kind != "answer" && input.Kind != "approval" {
		return result, &Error{Outcome: NotStarted}
	}
	path, err := resourcePath("runs", id, "/input")
	if err != nil {
		return result, err
	}
	err = c.call(ctx, http.MethodPost, path, auth, key, input, &result, true, true)
	if err == nil && (result.ID != id || result.Status == "" || result.Version <= 0) {
		err = &Error{Outcome: Unknown, Retryable: true}
	}
	return result, err
}
func (c *Client) Cancel(ctx context.Context, auth Auth, id, key string) (ControlReceipt, error) {
	return c.control(ctx, auth, id, key, "/cancel")
}
func (c *Client) Resume(ctx context.Context, auth Auth, id, key string) (ControlReceipt, error) {
	return c.control(ctx, auth, id, key, "/resume")
}
func (c *Client) control(ctx context.Context, auth Auth, id, key, action string) (ControlReceipt, error) {
	var result ControlReceipt
	path, err := resourcePath("runs", id, action)
	if err != nil {
		return result, err
	}
	err = c.call(ctx, http.MethodPost, path, auth, key, struct{}{}, &result, true, true)
	if err == nil && (result.ID != id || result.Status == "" || result.Version <= 0) {
		err = &Error{Outcome: Unknown, Retryable: true}
	}
	return result, err
}
func resourcePath(kind, id, suffix string) (string, error) {
	if id == "" || len(id) > 128 {
		return "", &Error{Outcome: NotStarted}
	}
	for _, ch := range id {
		if !strings.ContainsRune("ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_", ch) {
			return "", &Error{Outcome: NotStarted}
		}
	}
	return "/" + kind + "/" + id + suffix, nil
}

func (c *Client) call(
	ctx context.Context,
	method, path string,
	auth Auth,
	key string,
	input, result any,
	mutation, idempotent bool,
) error {
	if ctx.Err() != nil || auth.ServiceToken == "" || auth.SubjectToken != "" && auth.SessionID != "" {
		return &Error{Outcome: NotStarted}
	}
	if idempotent &&
		(len(key) == 0 || len(key) > 128 || strings.TrimSpace(key) != key || strings.ContainsAny(key, "\r\n")) {
		return &Error{Outcome: NotStarted}
	}
	body, err := json.Marshal(input)
	if err != nil {
		return &Error{Outcome: NotStarted}
	}
	var reader io.Reader
	if input != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+"/v1"+path, reader)
	if err != nil {
		return &Error{Outcome: NotStarted}
	}
	if mutation {
		// The durable worker owns retries. Transport retries can hide an unknown
		// first effect behind a later rejection of the same idempotency key.
		req.GetBody = nil
	}
	req.Header.Set("Authorization", "Bearer "+auth.ServiceToken)
	if auth.SubjectToken != "" {
		req.Header.Set("X-Subject-Token", auth.SubjectToken)
	}
	if auth.SessionID != "" {
		req.Header.Set("X-Auth-Session-Id", auth.SessionID)
	}
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	if input != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	outcome := Rejected
	if mutation {
		outcome = Unknown
	}
	resp, err := c.http.Do(req)
	if err != nil {
		var networkError *net.OpError
		if errors.As(err, &networkError) && networkError.Op == "dial" {
			return &Error{Outcome: NotStarted, Retryable: true}
		}
		return &Error{Outcome: outcome, Retryable: !mutation || idempotent}
	}
	defer resp.Body.Close()
	return decodeResponse(resp, result, mutation, idempotent)
}

func decodeResponse(resp *http.Response, result any, mutation bool, idempotent bool) error {
	outcome := Rejected
	if mutation {
		outcome = Unknown
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, MaxResponseBytes+1))
	if err != nil || len(data) > MaxResponseBytes {
		return &Error{StatusCode: resp.StatusCode, Outcome: outcome, Retryable: !mutation || idempotent}
	}
	var code string
	// Only this typed conflict is safe negative evidence after an idempotent Input replay.
	if resp.StatusCode == http.StatusConflict {
		var body struct {
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		if json.Unmarshal(data, &body) == nil && body.Error.Code == CodeRunTerminal {
			code = CodeRunTerminal
		}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		e := &Error{StatusCode: resp.StatusCode, Outcome: Rejected, Code: code}
		if resp.StatusCode >= http.StatusInternalServerError {
			e.Outcome = outcome
			e.Retryable = !mutation || idempotent
		}
		if resp.StatusCode == http.StatusTooManyRequests {
			e.Retryable = true
		}
		return e
	}
	if err = json.Unmarshal(data, result); err != nil {
		return &Error{StatusCode: resp.StatusCode, Outcome: outcome, Retryable: !mutation || idempotent}
	}
	return nil
}
