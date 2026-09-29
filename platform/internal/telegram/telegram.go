package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type User struct {
	ID           int64  `json:"id"`
	FirstName    string `json:"first_name"`
	LastName     string `json:"last_name,omitempty"`
	Username     string `json:"username,omitempty"`
	IsBot        bool   `json:"is_bot"`
	LanguageCode string `json:"language_code,omitempty"`
}
type Chat struct {
	ID       int64  `json:"id"`
	Type     string `json:"type"`
	Username string `json:"username,omitempty"`
}
type Button struct {
	Text   string  `json:"text"`
	Data   string  `json:"callback_data,omitempty"`
	URL    string  `json:"url,omitempty"`
	WebApp *WebApp `json:"web_app,omitempty"`
}
type WebApp struct {
	URL string `json:"url"`
}
type Markup struct {
	Rows [][]Button `json:"inline_keyboard"`
}
type Message struct {
	ReplyToMessage  *Message        `json:"reply_to_message,omitempty"`
	Contact         *Contact        `json:"contact,omitempty"`
	ForwardOrigin   *ForwardOrigin  `json:"forward_origin,omitempty"`
	Audio           *Audio          `json:"audio,omitempty"`
	Voice           *Voice          `json:"voice,omitempty"`
	Video           *Video          `json:"video,omitempty"`
	VideoNote       *VideoNote      `json:"video_note,omitempty"`
	Entities        []MessageEntity `json:"entities,omitempty"`
	CaptionEntities []MessageEntity `json:"caption_entities,omitempty"`
	Sticker         *Sticker        `json:"sticker,omitempty"`
	Document        *Document       `json:"document,omitempty"`
	Photo           []PhotoSize     `json:"photo,omitempty"`
	Caption         string          `json:"caption,omitempty"`
	ID              int64           `json:"message_id"`
	ThreadID        int64           `json:"message_thread_id,omitempty"`
	Chat            Chat            `json:"chat"`
	From            User            `json:"from"`
	Text            string          `json:"text"`
	Markup          Markup          `json:"reply_markup"`
}

// Contact contains Telegram wire metadata for a shared user's identity.
// Phone numbers and hidden sender names are not identity evidence for invitations.
type Contact struct {
	UserID    int64  `json:"user_id,omitempty"`
	FirstName string `json:"first_name"`
}
type ForwardOrigin struct {
	Type       string `json:"type"`
	SenderUser *User  `json:"sender_user,omitempty"`
}
type Callback struct {
	ID      string  `json:"id"`
	From    User    `json:"from"`
	Data    string  `json:"data"`
	Message Message `json:"message"`
}
type Update struct {
	ID       int64     `json:"update_id"`
	Message  *Message  `json:"message,omitempty"`
	Callback *Callback `json:"callback_query,omitempty"`
}
type Send struct {
	NativeMarkdown bool   `json:"-"`
	LiteralPrefix  string `json:"-"`
	LiteralSuffix  string `json:"-"`
	ParseMode      string `json:"parse_mode,omitempty"`
	ChatID         int64  `json:"chat_id"`
	MessageID      int64  `json:"message_id,omitempty"`
	Text           string `json:"text"`
	Markup         Markup `json:"reply_markup"`
}

// ResponseParameters contains structured Telegram recovery information.
type ResponseParameters struct {
	RetryAfter int64 `json:"retry_after,omitempty"`
}

type APIError struct {
	Parameters  ResponseParameters
	Code        int
	Description string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("Telegram API error %d: %s", e.Code, e.Description)
}

type Client struct {
	Base  string
	Token string
	HTTP  *http.Client
}

const (
	requestTimeout   = 15 * time.Second
	maxResponseBytes = 2 << 20
)

func (c Client) Call(ctx context.Context, method string, in, out any) error {
	b, e := json.Marshal(in)
	if e != nil {
		return e
	}
	return c.request(ctx, method, bytes.NewReader(b), "application/json", out)
}

func (c Client) request(ctx context.Context, method string, body io.Reader, contentType string, out any) error {
	r, e := http.NewRequestWithContext(ctx, http.MethodPost, c.Base+"/bot"+c.Token+"/"+method, body)
	if e != nil {
		return e
	}
	r.Header.Set("Content-Type", contentType)
	client := c.HTTP
	if client == nil {
		client = &http.Client{Timeout: requestTimeout}
	}
	resp, e := client.Do(r)
	if e != nil {
		return fmt.Errorf("telegram transport unavailable")
	}
	defer resp.Body.Close()
	var envelope struct {
		OK          bool               `json:"ok"`
		Result      json.RawMessage    `json:"result"`
		Code        int                `json:"error_code"`
		Description string             `json:"description"`
		Parameters  ResponseParameters `json:"parameters"`
	}
	if e = json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes)).Decode(&envelope); e != nil {
		return e
	}
	if !envelope.OK {
		return &APIError{Code: envelope.Code, Description: envelope.Description, Parameters: envelope.Parameters}
	}
	if out != nil {
		return json.Unmarshal(envelope.Result, out)
	}
	return nil
}
func (c Client) Updates(ctx context.Context, offset int64) ([]Update, error) {
	var u []Update
	e := c.Call(
		ctx,
		"getUpdates",
		map[string]any{"offset": offset, "timeout": 1, "allowed_updates": []string{"message", "callback_query"}},
		&u,
	)
	return u, e
}
func (c Client) Send(ctx context.Context, p Send) (Message, error) {
	var err error
	p, err = PrepareSend(p)
	if err != nil {
		return Message{}, err
	}
	var m Message
	e := c.Call(ctx, "sendMessage", p, &m)
	return m, e
}
func (c Client) Edit(ctx context.Context, p Send) error {
	var err error
	p, err = PrepareSend(p)
	if err != nil {
		return err
	}
	return c.Call(ctx, "editMessageText", p, nil)
}
