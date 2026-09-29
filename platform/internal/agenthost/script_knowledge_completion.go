package agenthost

import (
	"bytes"
	"context"
	"encoding/json"
	"slices"

	"github.com/complynx/zns-chatbot/platform/internal/derivedmutation"
	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

type scriptKnowledgeReceiptReader interface {
	KnowledgeReceipt(
		context.Context,
		string,
		knowledge.Command,
		readsource.Derivation,
	) (derivedmutation.Receipt[knowledge.Result], error)
}

func (policy ScriptAuthorization) KnowledgeReceipt(ctx context.Context, owner string, command knowledge.Command,
	source readsource.Derivation) (derivedmutation.Receipt[knowledge.Result], error) {
	reader, ok := policy.ScriptDomainAuthority.(scriptKnowledgeReceiptReader)
	if !ok {
		return derivedmutation.Receipt[knowledge.Result]{}, nil
	}
	return reader.KnowledgeReceipt(ctx, owner, command, source)
}

// Recover only admitted commands whose callback did not persist its outcome.
// The run stays interrupted: a receipt proves one effect, never arbitrary VM output
// or completion of later calls. Use the live turn context after the worker joins.
func (s ScriptHost) recoverKnowledgeCalls(ctx context.Context, owner string, updateID int64) error {
	reader, ok := s.Store.Policy.(scriptKnowledgeReceiptReader)
	if !ok {
		return nil
	}
	snapshot, err := s.Store.ledgerSnapshot(ctx, owner, updateID)
	if err != nil {
		return err
	}
	if !interruptedMemoCalls(snapshot.records) {
		return nil
	}
	records, err := s.Store.Records(ctx, owner, updateID)
	if err != nil {
		return err
	}
	for index, record := range records {
		if scriptRetired(record) || record.Run.Error != scriptInterrupted {
			continue
		}
		for sequence, call := range record.Calls {
			if !recoverableMemoCall(call) {
				continue
			}
			if err = s.recoverKnowledgeCall(ctx, owner, updateID, index, sequence, call, reader); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s ScriptHost) recoverKnowledgeCall(ctx context.Context, owner string, updateID int64, index, sequence int,
	call ScriptToolRecord, reader scriptKnowledgeReceiptReader) error {
	receipt, err := reader.KnowledgeReceipt(ctx, owner, *call.Memory, *call.Source)
	if err != nil {
		return err
	}
	if !receipt.Found || receipt.Result.Redacted || receipt.Result.Memo == nil {
		return nil
	}
	call.Outcome.Result, err = json.Marshal(struct {
		Committed bool  `json:"committed"`
		Version   int64 `json:"version"`
	}{Committed: true, Version: receipt.Result.Memo.Version})
	if err != nil {
		return err
	}
	call.ResultAuthorities, err = readsource.Merge(receipt.Result.ReadAuthorities, receipt.Result.Memo.ReadAuthorities)
	if err != nil {
		return err
	}
	call.Outcome.Error = ""
	call.KnowledgeRefreshPending = true
	return s.Store.CompleteCall(ctx, owner, updateID, index, sequence, call)
}

// drainKnowledgeRefresh keeps presentation retry separate from mutation truth.
// A failed refresh remains pending in the already committed call. Every retry
// reloads current authorized records; retirement never becomes a refresh request.
func (s ScriptHost) drainKnowledgeRefresh(ctx context.Context, owner string, updateID int64) error {
	if s.RefreshKnowledge == nil {
		return nil
	}
	snapshot, err := s.Store.ledgerSnapshot(ctx, owner, updateID)
	if err != nil {
		return err
	}
	if !pendingKnowledgeRefresh(snapshot.records) {
		return nil
	}
	records, err := s.Store.Records(ctx, owner, updateID)
	if err != nil {
		return err
	}
	for index, record := range records {
		if scriptRetired(record) {
			continue
		}
		for sequence, call := range record.Calls {
			if !call.KnowledgeRefreshPending || call.Memory == nil || call.Outcome.Error != "" {
				continue
			}
			command := knowledge.Command{Name: call.Memory.Name, Event: call.Memory.Event}
			if err = s.RefreshKnowledge(ctx, owner, command); err != nil {
				// Presentation failure leaves the marker pending. It must not
				// turn an already committed domain outcome into a failed call.
				return ctx.Err()
			}
			if err = s.Store.completeKnowledgeRefresh(ctx, owner, updateID, index, sequence, call); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s ScriptStore) completeKnowledgeRefresh(ctx context.Context, owner string, updateID int64,
	index, sequence int, call ScriptToolRecord) error {
	identity, err := scriptCallIdentity(call)
	if err != nil {
		return err
	}
	return s.updateLedger(ctx, owner, updateID, index, func(records []ScriptRecord, _ int64) error {
		if sequence < 0 || sequence >= len(records[index].Calls) {
			return ErrScriptLedgerConflict
		}
		current := &records[index].Calls[sequence]
		currentIdentity, identityErr := scriptCallIdentity(*current)
		if identityErr != nil {
			return identityErr
		}
		if !bytes.Equal(identity, currentIdentity) || !bytes.Equal(call.Outcome.Result, current.Outcome.Result) ||
			current.Outcome.Error != "" {
			return ErrScriptLedgerConflict
		}
		current.KnowledgeRefreshPending = false
		return nil
	}, nil)
}

func knowledgeRefreshRequired(call ScriptToolRecord) bool {
	if call.Memory == nil {
		return false
	}
	switch call.Outcome.Name {
	case "knowledge.memo_set", "knowledge.memo_delete":
		return true
	default:
		return false
	}
}

func interruptedMemoCalls(records []ScriptRecord) bool {
	for _, record := range records {
		if scriptRetired(record) || record.Run.Error != scriptInterrupted {
			continue
		}
		if slices.ContainsFunc(record.Calls, recoverableMemoCall) {
			return true
		}
	}
	return false
}

func recoverableMemoCall(call ScriptToolRecord) bool {
	return call.Memory != nil && call.Source != nil && call.Outcome.Error == scriptInterrupted &&
		(call.Memory.Name == knowledge.MemoSet || call.Memory.Name == knowledge.MemoDelete)
}

func pendingKnowledgeRefresh(records []ScriptRecord) bool {
	for _, record := range records {
		if scriptRetired(record) {
			continue
		}
		for _, call := range record.Calls {
			if call.KnowledgeRefreshPending && call.Memory != nil && call.Outcome.Error == "" {
				return true
			}
		}
	}
	return false
}

// ownPrivateDeletion permits only a canonical, still-current deletion receipt to
// explain retirement. It never accepts VM output as evidence of an effect.
func (s ScriptHost) ownPrivateDeletion(ctx context.Context, owner string, updateID int64, index int) (bool, error) {
	reader, ok := s.Store.Policy.(scriptKnowledgeReceiptReader)
	if !ok {
		return false, nil
	}
	snapshot, err := s.Store.ledgerSnapshot(ctx, owner, updateID)
	if err != nil {
		return false, err
	}
	if index < 0 || index >= len(snapshot.records) {
		return false, nil
	}
	record := snapshot.records[index]
	if !scriptRetired(record) {
		return false, nil
	}
	for _, call := range record.Calls {
		if call.Memory == nil || call.Source == nil ||
			(call.Memory.Name != knowledge.MemoDelete && call.Memory.Name != knowledge.DocumentDelete) {
			continue
		}
		receipt, receiptErr := reader.KnowledgeReceipt(ctx, owner, *call.Memory, *call.Source)
		if receiptErr != nil {
			return false, receiptErr
		}
		if receipt.Found && receipt.Result.PrivateDeletion != nil && receipt.Result.PrivateDeletion.Current {
			return true, nil
		}
	}
	return false, nil
}
