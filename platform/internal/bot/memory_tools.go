package bot

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
	"github.com/complynx/zns-chatbot/platform/internal/scriptclient"
)

const maxMemoryToolBytes = 32 * 1024
const memoryToolWrite = "memory.write"
const memoryToolRead = "memory.read"

// The script result carries the selected evidence. Do not re-inject every full
// intermediate read into the next prompt; the durable host ledger retains it.
func memoryCallProjection(outcome agent.ScriptToolResult) agent.ScriptToolResult {
	if strings.HasPrefix(outcome.Name, "memory.") && outcome.Name != memoryToolWrite && outcome.Error == "" {
		outcome.Result = json.RawMessage(
			`{"read_completed":true,"payload_omitted":true,"evidence":"See script result; omission is not absence."}`,
		)
	}
	return outcome
}

func (b *Bot) finalizeMemorySources(ctx context.Context, owner string, updateID int64) error {
	records, err := b.scriptRecords(ctx, owner, updateID)
	if err != nil {
		return err
	}
	for _, record := range records {
		for _, call := range record.Calls {
			if call.Memory == nil {
				continue
			}
			// Interrupted callbacks may have committed. The domain receipt resolves
			// that uncertainty without repeating the mutation.
			if err = b.API.AttachMemorySources(
				ctx,
				owner,
				call.Memory.Key,
				updateID,
			); err != nil {
				return err
			}
		}
	}
	return nil
}

func memoryTools() []scriptclient.Tool {
	query := json.RawMessage(
		`{"type":"object","properties":{"namespace":{"enum":["all","shared","private"]},"event":{"type":"string"},"topic":{"type":"string"},"text":{"type":"string"},"mode":{"enum":["literal","regex","text"]},"cursor":{"type":"string"}},"additionalProperties":false}`,
	)
	ref := json.RawMessage(
		`{"type":"object","properties":{"ref":{"type":"string"},"cursor":{"type":"string"}},"required":["ref"],"additionalProperties":false}`,
	)
	return []scriptclient.Tool{
		{
			Name:        "memory.summary",
			Description: "Read semantic summary and topic navigation. More/incomplete means partial coverage.",
			InputSchema: query,
		},
		{
			Name:        "memory.index",
			Description: "Browse authorized topic records; follow next_cursor with unchanged query.",
			InputSchema: query,
		},
		{
			Name:        "memory.search",
			Description: "Search literal substring, RE2 regex, or lexical text. Empty pages may be incomplete; continue next_cursor.",
			InputSchema: query,
		},
		{
			Name:        memoryToolRead,
			Description: "Read an exact indexed version; follow chunk cursor. Refresh index on stale reference.",
			InputSchema: ref,
		},
		{
			Name:        "memory.history",
			Description: "Read bounded revision history of a visible reference, with continuation.",
			InputSchema: ref,
		},
		{
			Name:        "memory.revision",
			Description: "Read an explicit historical revision from history; follow chunk cursor. This is historical evidence, not current state.",
			InputSchema: ref,
		},
		{
			Name:        "memory.sources",
			Description: "Read actual owner-authorized source messages for this exact revision. Missing sources are not evidence.",
			InputSchema: ref,
		},
		{
			Name:        memoryToolWrite,
			Description: "Create/update/delete an owner-private document. For an existing document provide its current ref from memory.read. Host binds version and replay key. Shared knowledge still uses moderated knowledge actions.",
			InputSchema: json.RawMessage(
				`{"type":"object","properties":{"name":{"enum":["document_set","document_delete"]},"topic":{"type":"string"},"key":{"type":"string"},"text":{"type":"string"},"ref":{"type":"string"}},"required":["name","topic","key"],"additionalProperties":false}`,
			),
		},
	}
}

type memoryRefArguments struct {
	Ref    string `json:"ref"`
	Cursor string `json:"cursor"`
}

func (b *Bot) prepareMemoryTool(
	ctx context.Context,
	owner string,
	updateID int64,
	call scriptclient.ToolCall,
	record scriptToolRecord,
) (scriptToolRecord, error) {
	switch call.Name {
	case "memory.summary", "memory.index", "memory.search":
		return record, decodeScriptArguments(call.Arguments, new(knowledge.MemoryQuery))
	case memoryToolRead, "memory.history", "memory.revision", "memory.sources":
		var args memoryRefArguments
		if err := decodeScriptArguments(call.Arguments, &args); err != nil || args.Ref == "" {
			return record, errors.New("invalid memory reference")
		}
		return record, nil
	case memoryToolWrite:
		command, err := b.prepareMemoryWrite(ctx, owner, updateID, call.Arguments)
		record.Memory = command
		return record, err
	default:
		return record, errors.New("tool unavailable")
	}
}

func (b *Bot) prepareMemoryWrite(
	ctx context.Context,
	owner string,
	updateID int64,
	raw json.RawMessage,
) (*knowledge.Command, error) {
	var args struct {
		Name  string `json:"name"`
		Topic string `json:"topic"`
		Key   string `json:"key"`
		Text  string `json:"text"`
		Ref   string `json:"ref"`
	}
	if err := decodeScriptArguments(raw, &args); err != nil {
		return nil, err
	}
	if args.Name != knowledge.DocumentSet && args.Name != knowledge.DocumentDelete {
		return nil, errors.New("invalid memory action")
	}
	command := &knowledge.Command{Name: args.Name, Topic: args.Topic, FactKey: args.Key, Text: args.Text}
	if args.Ref != "" {
		if err := b.observedMemoryReference(ctx, owner, updateID, args.Ref); err != nil {
			return nil, err
		}
		var entry knowledge.MemoryEntry
		if err := b.API.memoryRead(ctx, owner, "read", url.Values{"ref": {args.Ref}}, &entry); err != nil {
			return nil, err
		}
		if entry.Namespace != knowledge.MemoryPrivate || entry.SourceKind != "document" || entry.Topic != args.Topic ||
			entry.Key != args.Key ||
			entry.Historical {
			return nil, errors.New("memory target requires a current private document")
		}
		command.Version = entry.Version
		return command, nil
	}
	var document knowledge.Document
	err := b.API.memoryRead(
		ctx,
		owner,
		"document",
		url.Values{"topic": {args.Topic}, knowledgeKeyQuery: {args.Key}},
		&document,
	)
	if err != nil {
		return nil, err
	}
	command.Version = document.Version
	if document.Active || args.Name == knowledge.DocumentDelete {
		return nil, errors.New("memory target requires a current read")
	}
	return command, nil
}

func (b *Bot) observedMemoryReference(ctx context.Context, owner string, updateID int64, reference string) error {
	records, err := b.scriptRecords(ctx, owner, updateID)
	if err != nil {
		return err
	}
	for _, run := range records {
		for _, call := range run.Calls {
			if call.Outcome.Name != memoryToolRead || call.Outcome.Error != "" {
				continue
			}
			var entry knowledge.MemoryEntry
			if json.Unmarshal(call.Outcome.Result, &entry) == nil && entry.Ref == reference && !entry.Historical {
				return nil
			}
		}
	}
	return errors.New("memory target requires a current host read")
}

func (b *Bot) executeMemoryTool(
	ctx context.Context,
	owner string,
	call scriptclient.ToolCall,
	record scriptToolRecord,
) (any, error) {
	if record.Memory != nil {
		result, err := b.API.ExecuteKnowledge(ctx, owner, *record.Memory)
		if err != nil {
			return nil, err
		}
		// Return mutation metadata; detailed text is available through versioned reads.
		if result.Document != nil {
			result.Document.Text = ""
		}
		return result, nil
	}
	operation := strings.TrimPrefix(call.Name, "memory.")
	var values url.Values
	if operation == "summary" || operation == "index" || operation == "search" {
		var query knowledge.MemoryQuery
		if err := decodeScriptArguments(call.Arguments, &query); err != nil {
			return nil, err
		}
		values = memoryQueryValues(query)
	} else {
		var args memoryRefArguments
		if err := decodeScriptArguments(call.Arguments, &args); err != nil {
			return nil, err
		}
		values = url.Values{"ref": {args.Ref}, memoryCursorQuery: {args.Cursor}}
	}
	var result json.RawMessage
	err := b.API.memoryRead(ctx, owner, operation, values, &result)
	return result, err
}
