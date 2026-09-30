package agenthost

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/derivedmutation"
	"github.com/complynx/zns-chatbot/platform/internal/interaction"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

var errScriptRegistrationCommitted = errors.New("script stopped after committed registration transition")

type scriptRegistrationReceiptReader interface {
	PassBookingReceipt(
		context.Context,
		string,
		passbooking.Command,
		readsource.Derivation,
	) (derivedmutation.Receipt[passbooking.Booking], error)
}

func (policy ScriptAuthorization) PassBookingReceipt(ctx context.Context, owner string, command passbooking.Command,
	source readsource.Derivation) (derivedmutation.Receipt[passbooking.Booking], error) {
	reader, ok := policy.ScriptDomainAuthority.(scriptRegistrationReceiptReader)
	if !ok {
		return derivedmutation.Receipt[passbooking.Booking]{}, nil
	}
	return reader.PassBookingReceipt(ctx, owner, command, source)
}

// A committed owner command can invalidate the exact booking it consumed. Check
// that transition on a detached authority clone, then discard every VM-derived
// payload. The rebased clone is never written or exposed to a model.
func (s ScriptStore) completeRegistrationCall(ctx context.Context, owner string, updateID int64,
	index, sequence int, call ScriptToolRecord) (bool, error) {
	if paymentRegistrationCompletion(call) {
		return s.completePaymentCall(ctx, owner, updateID, index, sequence, call)
	}
	reader, ok := s.Policy.(scriptRegistrationReceiptReader)
	if !ok || !ownerRegistrationCompletion(call) {
		return false, nil
	}
	receipt, err := reader.PassBookingReceipt(ctx, owner, *call.Pass.Command, *call.Source)
	if err != nil {
		return true, err
	}
	booking := receipt.Result
	command := call.Pass.Command
	if !receipt.Found || booking.Owner != owner || booking.Event != command.Event ||
		booking.Version != command.Version+1 || booking.CreatedAt.IsZero() {
		return false, nil
	}
	if !registrationResponseMatches(call, booking) {
		return true, s.StaleError
	}
	before, found := registrationBeforeIdentity(owner, call)
	if !found {
		return false, nil
	}
	return s.commitRegistrationTransition(ctx, owner, updateID, index, sequence, call, before, booking, nil)
}

// Commit the retired payloads and trusted receipt in the same ledger revision.
func (s ScriptStore) commitRegistrationTransition(
	ctx context.Context,
	owner string,
	updateID int64,
	index, sequence int,
	call ScriptToolRecord,
	before interaction.BookingIdentity,
	booking passbooking.Booking,
	payment *passbooking.PaymentCompletionReceipt,
) (bool, error) {
	var identity []byte
	for range scriptLedgerAttempts {
		snapshot, loadErr := s.ledgerSnapshot(ctx, owner, updateID)
		if loadErr != nil {
			return true, loadErr
		}
		identity, loadErr = s.matchRunIdentity(snapshot.records, index, identity)
		if loadErr != nil {
			return true, loadErr
		}
		eligible, prepareErr := s.prepareRegistrationCompletion(
			ctx,
			owner,
			snapshot.records,
			index,
			sequence,
			call,
			before,
			booking,
			payment,
		)
		if prepareErr != nil {
			if errors.Is(prepareErr, s.StaleError) {
				return true, s.retirementError(ctx, owner, updateID, prepareErr, snapshot.raw)
			}
			return true, prepareErr
		}
		if !eligible {
			return false, nil
		}
		committed, commitErr := s.commitLedger(ctx, owner, updateID, snapshot, true, nil)
		if commitErr != nil {
			return true, commitErr
		}
		if committed {
			return true, errScriptRegistrationCommitted
		}
	}
	return true, ErrScriptLedgerConflict
}

func ownerRegistrationCompletion(call ScriptToolRecord) bool {
	return call.Outcome.Error == "" && CommittedPassReceipt(call) && call.Pass.Command != nil &&
		strings.HasPrefix(call.Pass.Name, "passes.registration.") && call.Pass.Command.Target == "" &&
		call.Pass.Command.Version > 0 && call.Source != nil && call.Source.Valid()
}

func (s ScriptStore) prepareRegistrationCompletion(
	ctx context.Context,
	owner string,
	records []ScriptRecord,
	index, sequence int,
	completed ScriptToolRecord,
	before interaction.BookingIdentity,
	booking passbooking.Booking,
	payment *passbooking.PaymentCompletionReceipt,
) (bool, error) {
	record := &records[index]
	if sequence < 0 || sequence >= len(record.Calls) {
		return false, ErrScriptLedgerConflict
	}
	admitted, err := scriptCallIdentity(record.Calls[sequence])
	if err != nil {
		return false, err
	}
	candidate, err := scriptCallIdentity(completed)
	if err != nil {
		return false, err
	}
	if !bytes.Equal(admitted, candidate) {
		return false, ErrScriptLedgerConflict
	}
	validation, err := registrationValidationRecord(*record)
	if err != nil {
		return false, err
	}
	validation.Calls[sequence] = completed
	found, err := rebaseRegistrationValidation(owner, &validation, before, PassIdentity(booking), payment)
	if err != nil || !found {
		return false, err
	}
	generation, err := s.Policy.Generation(ctx, owner)
	if err != nil {
		return false, err
	}
	state, err := s.Policy.MemoryState(ctx, owner)
	if err != nil {
		return false, err
	}
	if generation != record.HistoryGeneration || state != record.MemoryState {
		return false, s.StaleError
	}
	retired, err := s.authorizeRegistrationTransition(
		ctx,
		owner,
		records,
		index,
		validation,
		before,
		PassIdentity(booking),
		payment,
	)
	if err != nil {
		return false, err
	}
	trusted, err := registrationReceiptCall(completed, booking, payment)
	if err != nil {
		return false, err
	}
	for _, position := range retired {
		omitRegistrationPayload(&records[position])
	}
	retainRegistrationReceipt(record, sequence, trusted)
	return true, s.authorizeNewRecord(ctx, owner, *record)
}

func registrationValidationRecord(record ScriptRecord) (ScriptRecord, error) {
	raw, err := json.Marshal(record)
	if err != nil {
		return ScriptRecord{}, err
	}
	var result ScriptRecord
	err = json.Unmarshal(raw, &result)
	return result, err
}

func rebaseRegistrationValidation(
	owner string,
	record *ScriptRecord,
	before, after interaction.BookingIdentity,
	payment *passbooking.PaymentCompletionReceipt,
) (bool, error) {
	found := replaceRegistrationAuthority(
		owner,
		record.HistoryGeneration,
		record.ReadAuthorities,
		before,
		after,
		payment,
	)
	for index := range record.PassContext {
		dependency := &record.PassContext[index]
		if dependency.Booking != nil && sameRegistrationIdentity(*dependency.Booking, before) {
			replacement := after
			dependency.Booking = &replacement
			found = true
		}
		found = replacePaymentQueueAuthority(dependency, payment) || found
	}
	for index := range record.Calls {
		call := &record.Calls[index]
		refs, err := ScriptCallResultAuthorities(*call)
		if err != nil {
			return false, err
		}
		call.ResultAuthorities = readsource.CloneAuthorities(refs)
		found = replaceRegistrationAuthority(
			owner,
			record.HistoryGeneration,
			call.ResultAuthorities,
			before,
			after,
			payment,
		) ||
			found
		if call.Source != nil {
			source := call.Source.Clone()
			call.Source = &source
			found = replaceRegistrationAuthority(
				owner,
				record.HistoryGeneration,
				source.Authorities,
				before,
				after,
				payment,
			) ||
				found
		}
	}
	return found, nil
}

func replaceRegistrationAuthority(owner string, generation int64, refs []readsource.Authority,
	before, after interaction.BookingIdentity, payment *passbooking.PaymentCompletionReceipt) bool {
	found := false
	for index := range refs {
		ref := &refs[index]
		if causal := ref.Causal; causal != nil {
			if causal.Actor == owner && !causal.Published && causal.Generation != nil &&
				*causal.Generation == generation {
				found = replaceRegistrationAuthority(owner, generation, causal.Authorities, before, after, payment) ||
					found
			}
			continue
		}
		old := ref.Registration
		identity := interaction.BookingIdentity{
			Owner:     old.Owner,
			Event:     old.Event,
			Version:   old.Version,
			CreatedAt: old.CreatedAt,
		}
		if payment != nil && samePaymentReadAuthority(old, payment.Before) &&
			sameRegistrationIdentity(identity, before) {
			ref.Registration = payment.After
			found = true
			continue
		}
		if old.Kind == passbooking.ReadOwnerBooking && sameRegistrationIdentity(identity, before) {
			ref.Registration = BookingReadAuthority(after)
			found = true
		}
	}
	return found
}

// A payment review read captured by an earlier run holds the exact pending
// attempt. Rebase only that entry in the detached copy; every other queue item
// and the queue read request itself remain checked against current authority.
func replacePaymentQueueAuthority(
	dependency *interaction.PassContextDependency,
	payment *passbooking.PaymentCompletionReceipt,
) bool {
	if payment == nil || len(dependency.QueueAuthorities) == 0 {
		return false
	}
	found := false
	refs := append([]passbooking.ReadAuthority{}, dependency.QueueAuthorities...)
	for index := range refs {
		if samePaymentReadAuthority(refs[index], payment.Before) {
			refs[index] = payment.After
			found = true
		}
	}
	if found {
		dependency.QueueAuthorities = refs
	}
	return found
}

func sameRegistrationIdentity(a, b interaction.BookingIdentity) bool {
	return a.Owner == b.Owner && a.Event == b.Event && a.Version == b.Version && a.CreatedAt.Equal(b.CreatedAt)
}

func registrationReceiptCall(
	call ScriptToolRecord,
	booking passbooking.Booking,
	payment *passbooking.PaymentCompletionReceipt,
) (ScriptToolRecord, error) {
	if payment != nil {
		return paymentReceiptCall(call, *payment)
	}
	trusted, err := cloneScriptCall(call)
	if err != nil {
		return trusted, err
	}
	result := struct {
		ID       string              `json:"operation_id"`
		Complete bool                `json:"complete"`
		Result   passbooking.Booking `json:"result"`
	}{ID: call.Pass.ID, Complete: true, Result: booking}
	trusted.Outcome.Result, err = json.Marshal(ModelToolEvidence(result))
	trusted.ResultAuthorities = readsource.Registration(
		[]passbooking.ReadAuthority{BookingReadAuthority(PassIdentity(booking))},
	)
	return trusted, err
}

// omitRegistrationPayload disposes stale output after an exact own transition.
// It preserves admissions and every independently recorded retirement flag.
func omitRegistrationPayload(record *ScriptRecord) {
	record.Request = agent.ScriptProposal{}
	record.ReadAuthorities = []readsource.Authority{}
	record.PassContext = []interaction.PassContextDependency{}
	for index := range record.Calls {
		call := &record.Calls[index]
		call.Outcome.Result = nil
		call.Outcome.Error = "source_changed"
		scrubRetiredCall(call)
	}
	record.Run = agent.ScriptRun{Result: json.RawMessage(`{"omitted":true,"reason":"source_changed"}`)}
}

func retainRegistrationReceipt(record *ScriptRecord, sequence int, trusted ScriptToolRecord) {
	omitRegistrationPayload(record)
	record.Calls[sequence] = trusted
	// This is a host receipt, not an evaluation result or a current-state claim.
	status, _ := json.Marshal(struct {
		ID       string `json:"operation_id"`
		Complete bool   `json:"complete"`
	}{ID: trusted.Pass.ID, Complete: true})
	record.Run = agent.ScriptRun{Result: status}
}

// The derived executor checked this admitted source before committing the exact
// command. Re-registration may create a new booking incarnation; its receipt
// supplies the after identity, while this source supplies the before identity.
func registrationBeforeIdentity(owner string, call ScriptToolRecord) (interaction.BookingIdentity, bool) {
	var result interaction.BookingIdentity
	found, ambiguous := false, false
	command := call.Pass.Command
	inspect := func(ref readsource.Authority) {
		a := ref.Registration
		if a.Kind != passbooking.ReadOwnerBooking || a.Owner != owner || a.Event != command.Event ||
			a.Version != command.Version {
			return
		}
		identity := interaction.BookingIdentity{
			Owner:     a.Owner,
			Event:     a.Event,
			Version:   a.Version,
			CreatedAt: a.CreatedAt,
		}
		if found && !sameRegistrationIdentity(result, identity) {
			ambiguous = true
		}
		result, found = identity, true
	}
	for _, ref := range call.Source.Authorities {
		if ref.Causal == nil {
			inspect(ref)
			continue
		}
		causal := ref.Causal
		if causal.Actor == owner && !causal.Published && causal.Generation != nil &&
			*causal.Generation == *call.Source.Generation {
			for _, leaf := range causal.Authorities {
				inspect(leaf)
			}
		}
	}
	return result, found && !ambiguous && !result.CreatedAt.IsZero()
}

func registrationResponseMatches(call ScriptToolRecord, current passbooking.Booking) bool {
	var response struct {
		ID       string              `json:"operation_id"`
		Complete bool                `json:"complete"`
		Result   passbooking.Booking `json:"result"`
	}
	return json.Unmarshal(call.Outcome.Result, &response) == nil && response.Complete &&
		response.ID == call.Pass.ID && PassIdentity(response.Result).Matches(current)
}

func (s ScriptStore) authorizeRegistrationTransition(
	ctx context.Context,
	owner string,
	records []ScriptRecord,
	index int,
	validation ScriptRecord,
	before, after interaction.BookingIdentity,
	payment *passbooking.PaymentCompletionReceipt,
) ([]int, error) {
	retired := []int{}
	for position, previous := range records {
		if scriptRetired(previous) {
			continue
		}
		if previous.HistoryGeneration != validation.HistoryGeneration ||
			previous.MemoryState != validation.MemoryState {
			return nil, s.StaleError
		}
		if position == index {
			previous = validation
		} else {
			clone, err := registrationValidationRecord(previous)
			if err != nil {
				return nil, err
			}
			affected, err := rebaseRegistrationValidation(owner, &clone, before, after, payment)
			if err != nil {
				return nil, err
			}
			if affected {
				retired = append(retired, position)
			}
			previous = clone
		}
		if err := s.authorizeNewRecord(ctx, owner, previous); err != nil {
			return nil, err
		}
	}
	return retired, nil
}

type scriptPaymentReceiptReader interface {
	PassPaymentReceipt(
		context.Context,
		string,
		passbooking.Command,
		readsource.Derivation,
	) (passbooking.PaymentCompletionReceipt, error)
}

func (policy ScriptAuthorization) PassPaymentReceipt(
	ctx context.Context,
	owner string,
	command passbooking.Command,
	source readsource.Derivation,
) (passbooking.PaymentCompletionReceipt, error) {
	reader, ok := policy.ScriptDomainAuthority.(scriptPaymentReceiptReader)
	if !ok {
		return passbooking.PaymentCompletionReceipt{}, nil
	}
	return reader.PassPaymentReceipt(ctx, owner, command, source)
}

// An admitted payment decision is completed from the canonical receipt both
// after a complete response and after an uncertain one whose reply was lost.
func paymentRegistrationCompletion(call ScriptToolRecord) bool {
	if call.Outcome.Error != "" || call.Pass == nil || call.Pass.Command == nil || call.Source == nil ||
		!call.Source.Valid() || (!CommittedPassReceipt(call) && !interruptedPassCall(call)) {
		return false
	}
	command := call.Pass.Command
	switch call.Pass.Name {
	case "passes.payments.accept", "passes.payments.reject":
		return command.Target != "" && command.TargetVersion > 0 && command.PaymentAttempt != "" &&
			command.Name == PassToolActions()[call.Pass.Name]
	default:
		return false
	}
}

// The executor reports this exact admission as uncertain; it may have committed.
func interruptedPassCall(call ScriptToolRecord) bool {
	var response struct {
		ID          string `json:"operation_id"`
		Complete    bool   `json:"complete"`
		Interrupted bool   `json:"interrupted"`
	}
	return json.Unmarshal(call.Outcome.Result, &response) == nil && response.Interrupted && !response.Complete &&
		response.ID != "" && response.ID == call.Pass.ID
}

func (s ScriptStore) completePaymentCall(
	ctx context.Context,
	owner string,
	updateID int64,
	index, sequence int,
	call ScriptToolRecord,
) (bool, error) {
	reader, ok := s.Policy.(scriptPaymentReceiptReader)
	if !ok {
		return false, nil
	}
	receipt, err := reader.PassPaymentReceipt(ctx, owner, *call.Pass.Command, *call.Source)
	if err != nil {
		return true, err
	}
	if !paymentReceiptMatches(*call.Pass.Command, receipt) {
		return false, nil
	}
	before := interaction.BookingIdentity{
		Owner:     receipt.Before.Owner,
		Event:     receipt.Before.Event,
		Version:   receipt.Before.Version,
		CreatedAt: receipt.Before.CreatedAt,
	}
	after := passbooking.Booking{
		Owner:     receipt.After.Owner,
		Event:     receipt.After.Event,
		Version:   receipt.After.Version,
		CreatedAt: receipt.After.CreatedAt,
	}
	if !CommittedPassReceipt(call) {
		// The reply was lost after dispatch. Validate the probed canonical receipt,
		// never the uncertain response, and never dispatch the command again.
		call, err = paymentReceiptCall(call, receipt)
		if err != nil {
			return true, err
		}
	}
	return s.commitRegistrationTransition(ctx, owner, updateID, index, sequence, call, before, after, &receipt)
}

// The receipt must describe exactly the admitted target, attempt and version.
func paymentReceiptMatches(command passbooking.Command, receipt passbooking.PaymentCompletionReceipt) bool {
	before, after := receipt.Before, receipt.After
	return receipt.Found && receipt.Decision != "" && receipt.Attempt == command.PaymentAttempt &&
		before.PaymentAttempt == command.PaymentAttempt && before.Owner == command.Target &&
		before.Event == command.Event && before.Version == command.TargetVersion && !before.CreatedAt.IsZero() &&
		after.Owner == before.Owner && after.Event == before.Event && after.Version == before.Version+1 &&
		after.CreatedAt.Equal(before.CreatedAt) && after.PaymentAttempt == ""
}

// This host receipt exposes the committed decision, never the retired review page.
func paymentReceiptCall(call ScriptToolRecord, receipt passbooking.PaymentCompletionReceipt) (ScriptToolRecord, error) {
	trusted, err := cloneScriptCall(call)
	if err != nil {
		return trusted, err
	}
	result := map[string]any{
		"operation_id": call.Pass.ID,
		"complete":     true,
		"result":       map[string]string{"decision": receipt.Decision},
	}
	trusted.Outcome.Result, err = json.Marshal(ModelToolEvidence(result))
	trusted.ResultAuthorities = readsource.Registration([]passbooking.ReadAuthority{receipt.After})
	return trusted, err
}

func samePaymentReadAuthority(a, b passbooking.ReadAuthority) bool {
	if !a.CreatedAt.Equal(b.CreatedAt) {
		return false
	}
	a.CreatedAt = b.CreatedAt
	return a == b
}
