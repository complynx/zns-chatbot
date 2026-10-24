// Package telegram implements the small Bot API surface used by Shuttle.
package telegram

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

const MaxResponseBytes = 1 << 20
const defaultHTTPTimeout = 10 * time.Second
const maxHTTPErrorCode = 599

type User struct {
	ID           int64  `json:"id"`
	IsBot        bool   `json:"is_bot"`
	FirstName    string `json:"first_name"`
	LastName     string `json:"last_name,omitempty"`
	Username     string `json:"username,omitempty"`
	LanguageCode string `json:"language_code,omitempty"`
}
type Chat struct {
	ID   int64  `json:"id"`
	Type string `json:"type"`
}
type MessageEntity struct {
	Type          string `json:"type"`
	Offset        int    `json:"offset"`
	Length        int    `json:"length"`
	URL           string `json:"url,omitempty"`
	User          *User  `json:"user,omitempty"`
	Language      string `json:"language,omitempty"`
	CustomEmojiID string `json:"custom_emoji_id,omitempty"`
}
type MediaFile struct {
	FileID    string `json:"file_id"`
	UniqueID  string `json:"file_unique_id"`
	SizeBytes int64  `json:"file_size,omitempty"`
	Filename  string `json:"file_name,omitempty"`
	MIMEType  string `json:"mime_type,omitempty"`
	Width     int32  `json:"width,omitempty"`
	Height    int32  `json:"height,omitempty"`
}
type Message struct {
	Photo     []MediaFile `json:"photo,omitempty"`
	Document  *MediaFile  `json:"document,omitempty"`
	Audio     *MediaFile  `json:"audio,omitempty"`
	Voice     *MediaFile  `json:"voice,omitempty"`
	Video     *MediaFile  `json:"video,omitempty"`
	VideoNote *MediaFile  `json:"video_note,omitempty"`
	Animation *MediaFile  `json:"animation,omitempty"`

	ID             int64           `json:"message_id"`
	From           *User           `json:"from,omitempty"`
	Chat           Chat            `json:"chat"`
	Date           int64           `json:"date"`
	Text           string          `json:"text,omitempty"`
	Caption        string          `json:"caption,omitempty"`
	Entities       []MessageEntity `json:"entities,omitempty"`
	ReplyToMessage *Message        `json:"reply_to_message,omitempty"`
}
type CallbackQuery struct {
	ID      string   `json:"id"`
	From    User     `json:"from"`
	Message *Message `json:"message,omitempty"`
	Data    string   `json:"data,omitempty"`
}
type Update struct {
	ID            int64          `json:"update_id"`
	Message       *Message       `json:"message,omitempty"`
	CallbackQuery *CallbackQuery `json:"callback_query,omitempty"`
}
type WebAppInfo struct {
	URL string `json:"url"`
}

type InlineKeyboardButton struct {
	Text         string      `json:"text"`
	CallbackData string      `json:"callback_data,omitempty"`
	URL          string      `json:"url,omitempty"`
	WebApp       *WebAppInfo `json:"web_app,omitempty"`
}
type InlineKeyboardMarkup struct {
	InlineKeyboard [][]InlineKeyboardButton `json:"inline_keyboard"`
}
type SendMessageRequest struct {
	ChatID      int64           `json:"chat_id"`
	Text        string          `json:"text"`
	Entities    []MessageEntity `json:"entities,omitempty"`
	ReplyMarkup ReplyMarkup     `json:"reply_markup,omitempty"`
}
type EditMessageRequest struct {
	ChatID      int64                 `json:"chat_id"`
	MessageID   int64                 `json:"message_id"`
	Text        string                `json:"text"`
	Entities    []MessageEntity       `json:"entities,omitempty"`
	ReplyMarkup *InlineKeyboardMarkup `json:"reply_markup,omitempty"`
}
type Receipt struct {
	MessageID int64
	ChatID    int64
}

// Outcome distinguishes a rejected request from a delivery whose receipt was lost.
type Outcome string

const (
	NotStarted Outcome = "not_started"
	Rejected   Outcome = "rejected"
	Unknown    Outcome = "unknown"
)

type Error struct {
	fileTooLarge, fileInvalidID, fileAccessDenied bool
	StatusCode                                    int
	Retryable                                     bool
	RetryAfter                                    time.Duration
	Outcome                                       Outcome
	// InvalidEntities permits a plain text fallback after an explicit parsing rejection.
	InvalidEntities bool
}

func (e *Error) Error() string {
	return "Telegram request failed (" + string(e.Outcome) + ", status " + strconv.Itoa(e.StatusCode) + ")"
}

type Client struct {
	base  string
	token string
	http  *http.Client
}

func New(baseURL, token string, client *http.Client) (*Client, error) {
	u, err := url.Parse(baseURL)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.RawQuery != "" ||
		u.Fragment != "" {
		return nil, errors.New("invalid Telegram base URL")
	}
	botID, secret, found := strings.Cut(token, ":")
	if !found || botID == "" || secret == "" {
		return nil, errors.New("invalid Telegram token")
	}
	for _, ch := range token {
		if !strings.ContainsRune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_-:", ch) {
			return nil, errors.New("invalid Telegram token")
		}
	}
	h := http.Client{Timeout: defaultHTTPTimeout}
	if client != nil {
		h = *client
	}
	if h.Timeout <= 0 {
		h.Timeout = defaultHTTPTimeout
	}
	h.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Client{base: strings.TrimRight(baseURL, "/"), token: token, http: &h}, nil
}
func (c *Client) GetMe(ctx context.Context) (User, error) {
	var result User
	err := c.call(ctx, "getMe", struct{}{}, &result, false)
	if err == nil && result.ID <= 0 {
		err = &Error{Outcome: Rejected, Retryable: true}
	}
	return result, err
}
func (c *Client) SendMessage(ctx context.Context, input SendMessageRequest) (Receipt, error) {
	var message Message
	err := c.call(ctx, "sendMessage", input, &message, true)
	if err == nil && (message.ID <= 0 || message.Chat.ID != input.ChatID) {
		err = &Error{Outcome: Unknown}
	}
	return Receipt{MessageID: message.ID, ChatID: message.Chat.ID}, err
}
func (c *Client) EditMessageText(ctx context.Context, input EditMessageRequest) (Receipt, error) {
	var message Message
	err := c.call(ctx, "editMessageText", input, &message, true)
	if err == nil && (message.ID != input.MessageID || message.Chat.ID != input.ChatID) {
		err = &Error{Outcome: Unknown}
	}
	return Receipt{MessageID: message.ID, ChatID: message.Chat.ID}, err
}
func (c *Client) AnswerCallbackQuery(ctx context.Context, id, text string) error {
	var result bool
	return c.call(ctx, "answerCallbackQuery", struct {
		ID   string `json:"callback_query_id"`
		Text string `json:"text,omitempty"`
	}{id, text}, &result, true)
}
func (c *Client) call(ctx context.Context, method string, input, result any, mutation bool) error {
	if ctx.Err() != nil {
		return &Error{Outcome: NotStarted}
	}
	body, err := json.Marshal(input)
	if err != nil {
		return &Error{Outcome: NotStarted}
	}
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		c.base+"/bot"+c.token+"/"+method,
		bytes.NewReader(body),
	)
	if err != nil {
		return &Error{Outcome: NotStarted}
	}
	req.Header.Set("Content-Type", "application/json")
	return c.do(req, result, mutation)
}

func (c *Client) do(req *http.Request, result any, mutation bool) error {
	resp, err := c.http.Do(req)
	outcome := Rejected
	if mutation {
		outcome = Unknown
	}
	if err != nil {
		var networkError *net.OpError
		if errors.As(err, &networkError) && networkError.Op == "dial" {
			return &Error{Outcome: NotStarted, Retryable: true}
		}
		return &Error{Outcome: outcome, Retryable: !mutation}
	}
	defer resp.Body.Close()
	return decodeResponse(resp, result, mutation)
}

func decodeResponse(resp *http.Response, result any, mutation bool) error {
	outcome := Rejected
	if mutation {
		outcome = Unknown
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, MaxResponseBytes+1))
	if err != nil || len(data) > MaxResponseBytes {
		return &Error{StatusCode: resp.StatusCode, Outcome: outcome, Retryable: !mutation}
	}
	var envelope struct {
		OK          *bool           `json:"ok"`
		Result      json.RawMessage `json:"result"`
		Code        int             `json:"error_code"`
		Description string          `json:"description"`
		Parameters  struct {
			RetryAfter int64 `json:"retry_after"`
		} `json:"parameters"`
	}
	if err = json.Unmarshal(data, &envelope); err != nil {
		return &Error{StatusCode: resp.StatusCode, Outcome: outcome, Retryable: !mutation}
	}
	if envelope.OK == nil {
		return &Error{StatusCode: resp.StatusCode, Outcome: outcome, Retryable: !mutation}
	}
	if !*envelope.OK {
		if envelope.Code < http.StatusBadRequest || envelope.Code > maxHTTPErrorCode ||
			(resp.StatusCode != http.StatusOK && resp.StatusCode != envelope.Code) {
			return &Error{StatusCode: resp.StatusCode, Outcome: outcome, Retryable: !mutation}
		}
		return rejection(resp.StatusCode, envelope.Code, envelope.Description, envelope.Parameters.RetryAfter, mutation)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return &Error{StatusCode: resp.StatusCode, Outcome: outcome, Retryable: !mutation}
	}

	if err = json.Unmarshal(envelope.Result, result); err != nil {
		return &Error{StatusCode: resp.StatusCode, Outcome: outcome, Retryable: !mutation}
	}
	return nil
}

func rejection(status, code int, description string, retryAfter int64, mutation bool) error {
	if code == 0 {
		code = status
	}
	e := &Error{StatusCode: code, Outcome: Rejected}
	e.fileTooLarge = (status == http.StatusOK || status == http.StatusBadRequest) &&
		code == http.StatusBadRequest && description == "Bad Request: file is too big"
	e.fileInvalidID = (status == http.StatusOK || status == http.StatusBadRequest) &&
		code == http.StatusBadRequest && description == "Bad Request: invalid file_id"
	e.fileAccessDenied = (status == http.StatusOK || status == http.StatusForbidden) && code == http.StatusForbidden
	e.InvalidEntities = status == http.StatusBadRequest && code == http.StatusBadRequest &&
		strings.Contains(strings.ToLower(description), "can't parse entities")
	if code == http.StatusTooManyRequests {
		e.Retryable = true
		if retryAfter > 0 && retryAfter <= 86400 {
			e.RetryAfter = time.Duration(retryAfter) * time.Second
		}
	}
	if code >= http.StatusInternalServerError {
		if mutation {
			e.Outcome = Unknown
		}
		e.Retryable = !mutation
	}
	return e
}
