package agenthost

import (
	"bytes"
	"context"
	"errors"

	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

// Pure order reads may be repeated after cancellation. Only their completed
// page advances the durable cursor; no page leaves the host before that commit.
type scriptReadCall struct {
	identity []byte
	sequence int
	source   readsource.Derivation
}

func scriptPureOrderRead(name string) bool {
	return name == modernOrdersInspect || name == modernOrdersQuote
}

func (s ScriptStore) admitToolSource(ctx context.Context, owner string, updateID int64, index int,
	name string) (*scriptReadCall, error) {
	if scriptPureOrderRead(name) {
		return s.admitReadSource(ctx, owner, updateID, index)
	}
	return nil, s.AdmitSource(ctx, owner, updateID, index)
}

// Effectful calls retain their pre-execution receipt. Pure reads capture the
// same source boundary but publish only their completed receipt.
func (s ScriptStore) admitPreparedCall(ctx context.Context, owner string, updateID int64, index int,
	call *ScriptToolRecord, readCall *scriptReadCall) (int, error) {
	if readCall == nil {
		return s.AdmitCall(ctx, owner, updateID, index, call)
	}
	readCall.bind(updateID, index, call)
	return readCall.sequence, nil
}

func (s ScriptStore) admitReadSource(
	ctx context.Context,
	owner string,
	updateID int64,
	index int,
) (*scriptReadCall, error) {
	records, err := s.LoadAuthorized(ctx, owner, updateID)
	if err != nil {
		return nil, err
	}
	if index < 0 || index >= len(records) {
		return nil, errors.New("script reservation missing")
	}
	record := records[index]
	if scriptRetired(record) {
		return nil, s.StaleError
	}
	if len(record.Calls) >= MaxScriptCalls || record.Run.Error != scriptInterrupted {
		return nil, errors.New("script call budget exhausted")
	}
	identity, err := scriptRunIdentity(record)
	if err != nil {
		return nil, err
	}
	source, err := admittedScriptSource(owner, records, index, nil)
	if err != nil {
		return nil, err
	}
	if _, err = readsource.Capture(owner, *source); err != nil {
		return nil, err
	}
	return &scriptReadCall{identity: identity, sequence: len(record.Calls), source: *source}, nil
}

func (reservation *scriptReadCall) bind(updateID int64, index int, call *ScriptToolRecord) {
	source := reservation.source.Clone()
	call.Source = &source
	BindScriptToolKey(
		call,
		ScriptToolKey(updateID, index, reservation.sequence),
		index*MaxScriptCalls+reservation.sequence+1,
	)
}

func (s ScriptStore) completeReadCall(ctx context.Context, owner string, updateID int64, index int,
	reservation *scriptReadCall, call ScriptToolRecord) error {
	detached, err := cloneScriptCall(call)
	if err != nil {
		return err
	}
	return s.updateLedger(ctx, owner, updateID, index, func(records []ScriptRecord, _ int64) error {
		current := &records[index]
		identity, identityErr := scriptRunIdentity(*current)
		if identityErr != nil {
			return identityErr
		}
		if !bytes.Equal(identity, reservation.identity) || len(current.Calls) != reservation.sequence ||
			current.Run.Error != scriptInterrupted {
			return ErrScriptLedgerConflict
		}
		current.Calls = append(current.Calls, detached)
		return nil
	}, nil)
}
