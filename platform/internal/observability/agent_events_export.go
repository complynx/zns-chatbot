package observability

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"time"

	"github.com/google/uuid"
)

// AgentExportOptions selects a bounded window from one unchanged log file.
type AgentExportOptions struct {
	Since  time.Time
	Until  time.Time
	Offset int64
	Limit  int
}

// AgentExportPage reports omissions and a byte cursor in the same file.
type AgentExportPage struct {
	Version    int    `json:"version"`
	Records    int    `json:"records"`
	Skipped    int    `json:"skipped"`
	NextOffset int64  `json:"next_offset"`
	Incomplete bool   `json:"incomplete"`
	Reason     string `json:"reason"`
}

// ExportAgentLog emits only validated diagnostic records. It never exports other
// log attributes, raw source, parser errors, arguments or tool results.
func ExportAgentLog(
	ctx context.Context,
	input io.ReadSeeker,
	output io.Writer,
	options AgentExportOptions,
) (AgentExportPage, error) {
	page := AgentExportPage{Version: agentEventVersion, NextOffset: options.Offset, Reason: "end"}
	if !validExportOptions(options) {
		return page, errors.New("invalid export options")
	}
	if err := seekAgentLog(input, options.Offset); err != nil {
		return page, err
	}
	const maxScan = 8 << 20
	const maxLogLine = 64 << 10
	reader := bufio.NewReaderSize(io.LimitReader(input, maxScan+1), maxLogLine)
	encoder := json.NewEncoder(output)
	for page.Records < options.Limit {
		if err := ctx.Err(); err != nil {
			return page, err
		}
		line, err := reader.ReadSlice('\n')
		if page.NextOffset-options.Offset+int64(len(line)) > maxScan {
			page.Incomplete, page.Reason = true, "scan_limit"
			return page, nil
		}
		if err != nil {
			return finishAgentExport(page, line, err)
		}
		page.NextOffset += int64(len(line))
		record, ok := decodeAgentRecord(line)
		if !ok || record.At.Before(options.Since) || (!options.Until.IsZero() && !record.At.Before(options.Until)) {
			page.Skipped++
			continue
		}
		if err = encoder.Encode(record); err != nil {
			return page, errors.New("cannot write diagnostic export")
		}
		page.Records++
	}
	page.Incomplete, page.Reason = true, "record_limit"
	return page, nil
}

func validExportOptions(options AgentExportOptions) bool {
	return options.Offset >= 0 && options.Limit >= 1 && options.Limit <= 1000 &&
		(options.Until.IsZero() || !options.Until.Before(options.Since))
}

func finishAgentExport(page AgentExportPage, line []byte, err error) (AgentExportPage, error) {
	if errors.Is(err, io.EOF) {
		page.Incomplete = len(line) != 0
		if page.Incomplete {
			page.Reason = "partial_line"
		}
		return page, nil
	}
	page.Incomplete, page.Reason = true, "read_error"
	return page, errors.New("cannot read bounded log line")
}

func seekAgentLog(input io.ReadSeeker, offset int64) error {
	if offset > 0 {
		if _, err := input.Seek(offset-1, io.SeekStart); err != nil {
			return errors.New("invalid log cursor")
		}
		var preceding [1]byte
		if _, err := io.ReadFull(input, preceding[:]); err != nil || preceding[0] != '\n' {
			return errors.New("invalid log cursor")
		}
	}
	if _, err := input.Seek(offset, io.SeekStart); err != nil {
		return errors.New("cannot seek log")
	}
	return nil
}

func decodeAgentRecord(line []byte) (AgentRecord, bool) {
	var envelope struct {
		Event string `json:"agent_event"`
	}
	var record AgentRecord
	if json.Unmarshal(line, &envelope) != nil || len(envelope.Event) > 4096 ||
		json.Unmarshal([]byte(envelope.Event), &record) != nil {
		return record, false
	}
	id, err := uuid.Parse(record.Correlation)
	if err != nil || id == uuid.Nil || id.String() != record.Correlation || record.Version != agentEventVersion ||
		record.Attempt == 0 || record.Sequence == 0 || record.Sequence > maxAgentEvents || record.At.IsZero() {
		return AgentRecord{}, false
	}
	record.At = record.At.UTC()
	record.AgentEvent = safeAgentEvent(record.AgentEvent)
	record.ElapsedMS = max(0, min(record.ElapsedMS, maxDiagnosticDuration.Milliseconds()))
	if record.Parent >= record.Sequence {
		record.Parent = 0
	}
	if record.Code != nil {
		safeCodeProfile(record.Code)
	}
	return record, true
}

func safeCodeProfile(profile *CodeProfile) {
	profile.Status = diagnosticChoice(profile.Status, "omitted", codeStructural, "limited", "source_redacted")
	if profile.Source != "" {
		profile.Source = ProfileAgentCode(profile.Source).Source
	}
	profile.Bytes = max(0, min(profile.Bytes, maxDiagnosticBytes))
	profile.Nodes = max(0, min(profile.Nodes, maxCodeNodes))
	profile.Outline = profile.Outline[:min(len(profile.Outline), maxCodeOutline)]
	for i, kind := range profile.Outline {
		profile.Outline[i] = diagnosticChoice(
			kind,
			"loop",
			"function",
			"branch",
			"call",
			"return",
			"await",
			"assignment",
		)
	}
	profile.Methods = profile.Methods[:min(len(profile.Methods), maxCodeOutline)]
	for i, method := range profile.Methods {
		profile.Methods[i] = diagnosticChoice(method, "filter", "map", "sort", "reduce", "find", "some", "every",
			"flatMap", "slice", "includes", "join", "split", "$list", "$help")
	}
}
