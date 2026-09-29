package bot

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/scriptclient"
)

const scriptLineupQuery = "lineup.query"

func (b *Bot) scriptLineupEntry() scriptToolEntry {
	return scriptToolEntry{
		descriptor: scriptclient.Tool{
			Name:        scriptLineupQuery,
			Description: "Query the configured public DJ timetable: current/day/full. Host owns time and timezone; dates are event dates (before 07:00 belongs to previous day). Room is case-insensitive exact; DJ is case-insensitive substring. Follow next_cursor with identical filters while omitted=true; preserve continuation if the shared four lineup reads/update budget ends. Pages retain the existing 8 KiB cap; labels over 256 runes are explicitly truncated. Unavailable is not empty. Cursor is bound to startup snapshot, filters and first-page time; invalid/stale cursors require restarting. No reload tool.",
			InputSchema: json.RawMessage(
				`{"type":"object","properties":{"scope":{"enum":["current","day","full"]},"date":{"type":"string"},"room":{"type":"string","maxLength":128},"dj":{"type":"string","maxLength":128},"cursor":{"type":"string","maxLength":512}},"required":["scope"],"additionalProperties":false}`,
			),
		},
		prepare:     prepareScriptLineup,
		execute:     b.executeScriptLineup,
		resultLimit: maxScriptReadBytes,
	}
}

func prepareScriptLineup(
	_ context.Context,
	_ string,
	_ int64,
	call scriptclient.ToolCall,
	_ agent.Input,
) (scriptToolRecord, error) {
	record := scriptToolRecord{Outcome: agent.ScriptToolResult{Name: call.Name, Error: scriptInterrupted}}
	var query agent.LineupQuery
	if err := decodeScriptArguments(call.Arguments, &query); err != nil {
		return record, err
	}
	return record, query.Validate()
}

func (b *Bot) executeScriptLineup(
	_ context.Context,
	_ string,
	call scriptclient.ToolCall,
	_ scriptToolRecord,
	input *agent.Input,
) (any, error) {
	var query agent.LineupQuery
	if err := decodeScriptArguments(call.Arguments, &query); err != nil {
		return nil, err
	}
	if err := b.performLineupRead(query, input); err != nil {
		return nil, err
	}
	for _, read := range input.LineupSource.Reads {
		if read.Scope == query.Scope {
			return read, nil
		}
	}
	return nil, errors.New("lineup read unavailable")
}
