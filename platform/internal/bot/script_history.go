package bot

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/conversation"
	"github.com/complynx/zns-chatbot/platform/internal/scriptclient"
)

const scriptHistoryRead = "history.read"

type scriptHistoryArguments struct {
	EventID int64  `json:"event_id"`
	Cursor  string `json:"cursor,omitempty"`
}

type scriptHistoryChunk struct {
	conversation.TextChunk

	NextCursor string `json:"next_cursor"`
}

func (b *Bot) historyReadEntry() scriptToolEntry {
	return scriptToolEntry{
		descriptor: scriptclient.Tool{
			Name:        scriptHistoryRead,
			Description: "Read your archived event text in Unicode-safe chunks. Keep event_id unchanged and follow next_cursor while more is true. Concatenate text chunks. Omitted content cannot be recovered; stale means discard chunks and restart.",
			InputSchema: json.RawMessage(
				`{"type":"object","properties":{"event_id":{"type":"integer"},"cursor":{"type":"string"}},"required":["event_id"],"additionalProperties":false}`,
			),
		},
		prepare: prepareScriptHistoryRead, execute: b.executeScriptHistoryRead, resultLimit: maxScriptReadBytes,
	}
}

func prepareScriptHistoryRead(
	_ context.Context,
	_ string,
	_ int64,
	call scriptclient.ToolCall,
	_ agent.Input,
) (scriptToolRecord, error) {
	record := scriptToolRecord{Outcome: agent.ScriptToolResult{Name: call.Name, Error: scriptInterrupted}}
	var args scriptHistoryArguments
	if err := decodeScriptArguments(call.Arguments, &args); err != nil {
		return record, err
	}
	if args.EventID <= 0 {
		return record, errors.New("invalid history event")
	}
	return record, nil
}

func (b *Bot) executeScriptHistoryRead(
	ctx context.Context,
	owner string,
	call scriptclient.ToolCall,
	_ scriptToolRecord,
	_ *agent.Input,
) (any, error) {
	var args scriptHistoryArguments
	if err := decodeScriptArguments(call.Arguments, &args); err != nil {
		return nil, err
	}
	cursor, err := readScriptCursor(args.Cursor, owner, call.Name, strconv.FormatInt(args.EventID, 10))
	if err != nil {
		return nil, err
	}
	generation, err := b.API.historyGeneration(ctx, owner)
	if err != nil {
		return nil, err
	}
	if args.Cursor != "" && cursor.Version != generation {
		return nil, errScriptReadStale
	}
	query := url.Values{
		"offset": {strconv.Itoa(cursor.Offset)},
		"limit":  {strconv.Itoa(conversation.MaxChunkCharacters)},
		"digest": {cursor.Digest},
	}
	var chunk conversation.TextChunk

	err = b.API.call(
		ctx,
		owner,
		http.MethodGet,
		"/v1/me/history/"+strconv.FormatInt(args.EventID, 10)+"/text?"+query.Encode(),
		nil,
		&chunk,
	)
	if err != nil {
		return nil, scriptDomainAPIError(err)
	}
	current, err := b.API.historyGeneration(ctx, owner)
	if err != nil {
		return nil, err
	}
	if current != generation || chunk.Generation != generation {
		return nil, errScriptReadStale
	}
	result := scriptHistoryChunk{TextChunk: chunk}
	if chunk.More {
		cursor.Offset, cursor.Digest, cursor.Version = chunk.NextOffset, chunk.Digest, generation
		result.NextCursor = encodeScriptCursor(cursor)
	}
	return result, nil
}
