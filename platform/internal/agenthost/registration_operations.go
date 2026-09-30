package agenthost

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/interaction"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

const registrationOperationRows = `SELECT call.value->'pass' AS request, call.value->'source' AS source, i.created_at,
 COALESCE((run->>'pass_redacted')::boolean,false) OR COALESCE((run->>'history_redacted')::boolean,false)
 OR COALESCE((run->>'memory_redacted')::boolean,false) AS retired
 FROM bot.interactions i CROSS JOIN LATERAL jsonb_array_elements(i.content) run
 CROSS JOIN LATERAL jsonb_array_elements(run->'calls') call(value)
 WHERE i.owner=$1 AND i.kind='script_runs' AND call.value->'pass' IS NOT NULL`

type registrationAdmission struct {
	Request   ScriptPassRequest
	Source    *readsource.Derivation
	CreatedAt time.Time
	Retired   bool
}

func captureRegistrationWitness(owner string, call *ScriptToolRecord) error {
	request := call.Pass
	if request == nil {
		return nil
	}
	if request.Witness != nil {
		if !request.Witness.Valid(owner) {
			return errors.New("invalid admitted operation witness")
		}
		return nil
	}
	var witness passbooking.OperationWitness
	var err error
	switch {
	case request.Command != nil:
		witness, err = passbooking.CommandOperationWitness(owner, *request.Command)
	case request.Assignment != nil:
		witness, err = passbooking.AssignmentOperationWitness(owner, *request.Assignment)
	case request.Batch != nil:
		witness, err = passbooking.BatchOperationWitness(owner, *request.Batch)
	default:
		return nil
	}
	if err == nil {
		request.Witness = &witness
	}
	return err
}

// ReadRegistrationOperations projects original admissions from the host ledger.
// It never reads outcomes or reconstructs fields removed by privacy retirement.
func (s ScriptStore) registrationAdmissions(
	ctx context.Context,
	owner, exactID string,
) ([]registrationAdmission, error) {
	rows, err := s.DB.Query(ctx, `SELECT request,source,created_at,retired FROM (
 SELECT DISTINCT ON(request->>'id') request,source,created_at,retired FROM (`+registrationOperationRows+`) operations
 WHERE ($2='' OR request->>'id'=$2) ORDER BY request->>'id',created_at ASC) recent
 ORDER BY created_at DESC, request->>'id' LIMIT 20`, owner, exactID)
	if err != nil {
		return nil, core.DatabaseOperationContextError(ctx, err)
	}
	return collectRegistrationAdmissions(ctx, rows)
}

func collectRegistrationAdmissions(ctx context.Context, rows pgx.Rows) ([]registrationAdmission, error) {
	type admissionRow struct {
		Request   []byte
		Source    []byte
		CreatedAt time.Time
		Retired   bool
	}
	stored, err := pgx.CollectRows(rows, pgx.RowToStructByPos[admissionRow])
	if err != nil {
		return nil, core.DatabaseOperationContextError(ctx, err)
	}
	admitted := make([]registrationAdmission, 0, len(stored))
	for _, row := range stored {
		item := registrationAdmission{CreatedAt: row.CreatedAt, Retired: row.Retired}
		if row.Request != nil {
			if err = json.Unmarshal(row.Request, &item.Request); err != nil {
				return nil, err
			}
		}
		if row.Source != nil {
			if err = json.Unmarshal(row.Source, &item.Source); err != nil {
				return nil, err
			}
		}
		admitted = append(admitted, item)
	}
	return admitted, nil
}

func (s ScriptStore) ReadRegistrationOperations(
	ctx context.Context,
	owner, exactID string,
) ([]interaction.RegistrationOperation, error) {
	admitted, err := s.registrationAdmissions(ctx, owner, exactID)
	if err != nil {
		return nil, err
	}
	return projectRegistrationAdmissions(admitted), nil
}

// ReadRegistrationOperationsForUpdate selects only original admissions from one
// owner/update, in run/call order. It grants no authority to execute them.
func (s ScriptStore) ReadRegistrationOperationsForUpdate(
	ctx context.Context, owner string, update int64,
) ([]interaction.RegistrationOperation, error) {
	if update <= 0 {
		return nil, errors.New("invalid registration operation update")
	}
	const limit = agent.MaxScriptRuns * MaxScriptCalls
	rows, err := s.DB.Query(ctx, `SELECT call.value->'pass',call.value->'source',i.created_at,
 COALESCE((run.value->>'pass_redacted')::boolean,false)
 OR COALESCE((run.value->>'history_redacted')::boolean,false)
 OR COALESCE((run.value->>'memory_redacted')::boolean,false)
 FROM (SELECT content,created_at FROM bot.interactions
 WHERE owner=$1 AND update_id=$2 AND kind='script_runs') i
 CROSS JOIN LATERAL jsonb_array_elements(i.content) WITH ORDINALITY run(value,position)
 CROSS JOIN LATERAL jsonb_array_elements(run.value->'calls') WITH ORDINALITY call(value,position)
 WHERE call.value->'pass' IS NOT NULL
 ORDER BY run.position,call.position LIMIT $3`, owner, update, limit+1)
	if err != nil {
		return nil, core.DatabaseOperationContextError(ctx, err)
	}
	admitted, err := collectRegistrationAdmissions(ctx, rows)
	if err != nil {
		return nil, err
	}
	if len(admitted) > limit {
		return nil, errors.New("registration operation admission limit exceeded")
	}
	seen := make(map[string]bool, len(admitted))
	for _, admission := range admitted {
		id := admission.Request.ID
		if id == "" {
			continue
		}
		if seen[id] {
			return nil, errors.New("ambiguous registration operation admission")
		}
		seen[id] = true
	}
	return projectRegistrationAdmissions(admitted), nil
}

func projectRegistrationAdmissions(admitted []registrationAdmission) []interaction.RegistrationOperation {
	result := make([]interaction.RegistrationOperation, 0, len(admitted))
	for _, row := range admitted {
		request := row.Request
		if !validRegistrationAdmission(request) {
			continue
		}
		item := interaction.RegistrationOperation{
			ID: request.ID, Tool: request.Name, AdmittedAt: row.CreatedAt,
			Command: request.Command, Assignment: request.Assignment, Batch: request.Batch,
			Source:  row.Source,
			Witness: request.Witness, Retired: row.Retired,
		}
		if request.Menu != nil {
			item.Menu = &interaction.RegistrationOperationMenu{
				Event:      request.Menu.Event,
				Historical: request.Menu.Historical,
			}
		}
		result = append(result, item)
	}
	return result
}

// ReadRegistrationOperation returns only an executable original admission.
// Current domain authorization remains mandatory; witnesses cannot revive intent.
func (s ScriptStore) ReadRegistrationOperation(
	ctx context.Context,
	owner, id string,
) (*ScriptPassRequest, *readsource.Derivation, error) {
	if id == "" {
		return nil, nil, errors.New("missing registration operation reference")
	}
	admitted, err := s.registrationAdmissions(ctx, owner, id)
	if err != nil {
		return nil, nil, err
	}
	if len(admitted) != 1 {
		return nil, nil, errors.New("registration operation unavailable")
	}
	original := admitted[0]
	if original.Retired || !validRegistrationAdmission(original.Request) || original.Source == nil ||
		!original.Source.Valid() {
		return nil, nil, errors.New("registration operation unavailable")
	}
	source := original.Source.Clone()
	return &original.Request, &source, nil
}

func validRegistrationAdmission(request ScriptPassRequest) bool {
	envelopes := 0
	for _, present := range []bool{request.Command != nil, request.Assignment != nil, request.Batch != nil, request.Menu != nil} {
		if present {
			envelopes++
		}
	}
	if request.ID == "" || envelopes > 1 {
		return false
	}
	if envelopes == 0 {
		return request.Name == hostPassesExport
	}
	if request.Menu != nil {
		return request.Name == "passes.registration.show" || request.Name == hostPassesExport
	}
	if request.Assignment != nil {
		return request.Name == hostPassesAdminAssign
	}
	action, known := PassToolActions()[request.Name]
	if !known {
		return false
	}
	batch := strings.HasPrefix(request.Name, "passes.batch.")
	if request.Batch != nil {
		return batch && request.Batch.Action == action
	}
	_, read := PassPrivilegedReadView(request.Name)
	return !batch && !read && request.Name != hostPassesTiers && request.Name != hostPassesAdminAssign &&
		request.Command.Name == action
}
