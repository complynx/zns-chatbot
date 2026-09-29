package agenthost

import (
	"encoding/json"
	"strings"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/conversation"
	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

const historyDeleted = "history_deleted"

func RedactHistoryScript(record *ScriptRecord, generation int64) bool {
	if record.HistoryGeneration == generation && !record.HistoryRedacted {
		return false
	}
	record.HistoryGeneration, record.HistoryRedacted = generation, true
	record.Request = agent.ScriptProposal{}
	record.Run.Code, record.Run.Error = "", historyDeleted
	record.Run.Result = json.RawMessage(`{"omitted":true,"reason":"history_deleted"}`)
	record.Run.Calls = nil
	for index := range record.Calls {
		record.Calls[index].Outcome.Result = nil
		record.Calls[index].Outcome.Error = historyDeleted
		scrubRetiredCall(&record.Calls[index])
	}
	return true
}

func RedactHistoryPages(pages []conversation.Page, generation int64) {
	for index := range pages {
		if pages[index].Generation != generation {
			pages[index] = conversation.Page{
				Events:     []conversation.Event{},
				Generation: generation,
				Error:      historyDeleted,
			}
		}
	}
}

func RedactHistoryInteraction(kind string, raw json.RawMessage, generation int64) (json.RawMessage, error) {
	if kind == scriptRunsKind {
		var records []ScriptRecord
		if err := json.Unmarshal(raw, &records); err != nil {
			return nil, err
		}
		for index := range records {
			RedactHistoryScript(&records[index], generation)
		}
		return json.Marshal(records)
	}
	var pages []conversation.Page
	if err := json.Unmarshal(raw, &pages); err != nil {
		return nil, err
	}
	RedactHistoryPages(pages, generation)
	return json.Marshal(pages)
}

func ClearRevokedHistoryContext(input *agent.Input, generation int64, revoked bool) {
	input.History = nil
	input.ReadAuthorities = nil
	if input.Conversation != nil {
		input.Conversation.Summary = conversation.Summary{}
		input.Conversation.Gap = true
		RedactHistoryPages(input.Conversation.Reads, generation)
		if revoked {
			for index := range input.Conversation.Reads {
				input.Conversation.Reads[index] = conversation.Page{
					Events:     []conversation.Event{},
					Generation: generation,
					Error:      historyDeleted,
				}
			}
		}
	}
	if input.Script != nil {
		input.Script.ReadAuthorities = nil
		for index := range input.Script.Runs {
			input.Script.Runs[index] = agent.ScriptRun{Error: historyDeleted, PassRedacted: true}
		}
	}
	input.HistoryGeneration = generation
}

func ScriptHasProfileWrite(record ScriptRecord) bool {
	if record.PrivateProfile {
		return true
	}
	for _, call := range record.Calls {
		if call.Profile != nil {
			return true
		}
	}
	return false
}

func RedactProfileScript(record *ScriptRecord) {
	if !ScriptHasProfileWrite(*record) {
		return
	}
	record.Request.Code = ""
	record.Request.InputJSON = ""
	record.Run.Code = ""
	record.Run.Result = json.RawMessage(
		`{"private_profile_script":true,"evidence":"Inspect host call outcomes; read profile.get for current state."}`,
	)
	for i := range record.Calls {
		call := &record.Calls[i]
		if call.Profile != nil {
			metadata := *call.Profile
			metadata.Value = ""
			call.Profile = &metadata
		}
		if call.Profile == nil && call.Language == nil && call.Outcome.Error == "" {
			call.Outcome.Result = json.RawMessage(`{"completed":true,"private_payload_omitted":true}`)
		}
	}
}

func InputPrivateHistory(input *agent.Input) bool {
	if len(input.History) > 0 {
		return true
	}
	if input.Conversation == nil {
		return false
	}
	if input.Conversation.Summary.Text != "" {
		return true
	}
	for _, page := range input.Conversation.Reads {
		if len(page.Events) > 0 {
			return true
		}
	}
	return false
}

func ScriptPrivateHistory(run ScriptRecord) bool {
	if run.PrivateHistory {
		return true
	}
	for _, call := range run.Calls {
		if call.Source != nil && call.Source.PrivateHistory {
			return true
		}
		if call.Outcome.Error != "" {
			continue
		}
		switch call.Outcome.Name {
		case hostHistoryRead:
			var chunk conversation.TextChunk
			if json.Unmarshal(call.Outcome.Result, &chunk) == nil && chunk.Text != "" {
				return true
			}
		case hostHistoryPage, "memory.sources":
			var page conversation.Page
			if json.Unmarshal(call.Outcome.Result, &page) == nil && len(page.Events) > 0 {
				return true
			}
		}
	}
	return false
}

func RedactKnowledgeReads(reads []agent.KnowledgeReadResult, state knowledge.MemoryDeletionState) {
	for index := range reads {
		if reads[index].MemoryState == state {
			continue
		}
		request := reads[index].Request
		request.Text = ""
		reads[index] = agent.KnowledgeReadResult{
			Request:     request,
			MemoryState: state,
			Error:       "memory_deleted",
			Omitted:     true,
		}
	}
}

func RedactDeletedScript(record *ScriptRecord, state knowledge.MemoryDeletionState) bool {
	if record.MemoryState == state && !record.MemoryRedacted {
		return false
	}
	record.MemoryState = state
	record.MemoryRedacted = true
	record.Request.Code, record.Request.InputJSON, record.Run.Code = "", "", ""
	record.Run.Result = json.RawMessage(`{"omitted":true,"reason":"memory_deleted"}`)
	record.Run.Calls = nil
	for index := range record.Calls {
		call := &record.Calls[index]
		scrubRetiredCall(call)
		call.Outcome.Result = nil
		if strings.HasPrefix(call.Outcome.Name, "memory.") ||
			strings.HasPrefix(call.Outcome.Name, "knowledge.") {
			call.Outcome.Result = json.RawMessage(`{"omitted":true,"reason":"memory_deleted"}`)
		}
	}
	return true
}

func RedactMemoryInteraction(
	kind string,
	content json.RawMessage,
	state knowledge.MemoryDeletionState,
) (json.RawMessage, error) {
	if kind == knowledgeReadsKind {
		var reads []agent.KnowledgeReadResult
		if err := json.Unmarshal(content, &reads); err != nil {
			return nil, err
		}
		RedactKnowledgeReads(reads, state)
		return json.Marshal(reads)
	}
	var records []ScriptRecord
	if err := json.Unmarshal(content, &records); err != nil {
		return nil, err
	}
	for index := range records {
		RedactDeletedScript(&records[index], state)
	}
	return json.Marshal(records)
}

func RedactPassScript(record *ScriptRecord) {
	record.PassRedacted = true
	record.Request = agent.ScriptProposal{}
	record.Run.Code = ""
	record.Run.Result = json.RawMessage(`{"omitted":true,"reason":"pass_access_changed"}`)
	record.Run.Calls = nil
	for index := range record.Calls {
		record.Calls[index].Outcome.Result = nil
		scrubRetiredCall(&record.Calls[index])
	}
}

// Retired intent is never executable again. Keep source/receipt identities and
// domain witnesses, not arbitrary text or prepared customer/assignment options.
func scrubRetiredCall(call *ScriptToolRecord) {
	if call.Memory != nil {
		command := *call.Memory
		command.Text = ""
		call.Memory = &command
	}
	if call.Profile != nil {
		profile := *call.Profile
		profile.Value = ""
		call.Profile = &profile
	}
	if call.Pass != nil {
		request := *call.Pass
		if request.Assignment != nil {
			assignment := retiredAssignment(*request.Assignment)
			request.Assignment = &assignment
		}
		if request.Batch != nil {
			batch := *request.Batch
			batch.Options = retiredAssignment(batch.Options)
			request.Batch = &batch
		}
		call.Pass = &request
	}
	if call.Order != nil {
		command := *call.Order
		command.Choice, command.Country = nil, ""
		call.Order = &command
	}
	if call.ModernChoice != nil {
		choice := *call.ModernChoice
		choice.Choice = orders.Choice{}
		call.ModernChoice = &choice
	}
	if call.Broadcast != nil {
		request := *call.Broadcast
		request.Command = ""
		call.Broadcast = &request
	}
}

func retiredAssignment(command passbooking.AdminAssignment) passbooking.AdminAssignment {
	return passbooking.AdminAssignment{Event: command.Event, Key: command.Key,
		Version: command.Version, Target: command.Target, TargetVersion: command.TargetVersion}
}
