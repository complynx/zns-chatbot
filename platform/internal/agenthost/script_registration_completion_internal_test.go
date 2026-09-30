package agenthost

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/interaction"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

func TestRegistrationCompletionRequiresUniqueAdmittedIncarnation(t *testing.T) {
	t.Parallel()
	before := interaction.BookingIdentity{
		Owner:     "alice",
		Event:     "dance",
		Version:   2,
		CreatedAt: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
	}
	old := readsource.Authority{Registration: BookingReadAuthority(before)}
	other := old
	other.Registration.CreatedAt = before.CreatedAt.Add(time.Second)
	generation := int64(4)
	for _, test := range []struct {
		name string
		refs []readsource.Authority
		want bool
	}{
		{"direct", []readsource.Authority{old}, true},
		{"ambiguous", []readsource.Authority{old, other}, false},
		{"own_causal", []readsource.Authority{{Causal: &readsource.CausalSource{Actor: "alice", Generation: &generation, Authorities: []readsource.Authority{old}}}}, true},
		{"foreign_causal", []readsource.Authority{{Causal: &readsource.CausalSource{Actor: "bob", Generation: &generation, Authorities: []readsource.Authority{old}}}}, false},
		{"published_causal", []readsource.Authority{{Causal: &readsource.CausalSource{Actor: "alice", Published: true, Generation: &generation, Authorities: []readsource.Authority{old}}}}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			call := ScriptToolRecord{
				Pass:   &ScriptPassRequest{Command: &passbooking.Command{Event: "dance", Version: 2}},
				Source: &readsource.Derivation{Generation: &generation, Authorities: test.refs},
			}
			got, ok := registrationBeforeIdentity("alice", call)
			assert.Equal(t, test.want, ok)
			if ok {
				assert.Equal(t, before, got)
			}
		})
	}
}

func TestRegistrationCompletionValidationLeavesOriginalSourcesIntact(t *testing.T) {
	t.Parallel()
	before := interaction.BookingIdentity{
		Owner:     "alice",
		Event:     "dance",
		Version:   2,
		CreatedAt: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
	}
	after := before
	after.Version++
	after.CreatedAt = after.CreatedAt.Add(time.Hour)
	generation := int64(4)
	refs := readsource.Registration([]passbooking.ReadAuthority{
		BookingReadAuthority(before), {Kind: passbooking.ReadCapability, Event: "dance", Action: "admin_cancel"},
	})
	record := ScriptRecord{HistoryGeneration: generation, ReadAuthorities: refs,
		PassContext: []interaction.PassContextDependency{{Booking: &before}},
		Calls: []ScriptToolRecord{{Source: &readsource.Derivation{Generation: &generation, Authorities: refs},
			ResultAuthorities: refs, Outcome: agent.ScriptToolResult{Result: json.RawMessage(`{"private":"old"}`)}}}}
	original, err := json.Marshal(record)
	require.NoError(t, err)
	clone, err := registrationValidationRecord(record)
	require.NoError(t, err)
	found, err := rebaseRegistrationValidation("alice", &clone, before, after, nil)
	require.NoError(t, err)
	require.True(t, found)
	unchanged, err := json.Marshal(record)
	require.NoError(t, err)
	assert.JSONEq(t, string(original), string(unchanged))
	assert.Equal(t, after, *clone.PassContext[0].Booking)
	assert.Equal(t, BookingReadAuthority(after), clone.ReadAuthorities[0].Registration)
	assert.Equal(t, refs[1], clone.ReadAuthorities[1], "unrelated capability remains checked")
}

func TestRegistrationCompletionRequiresMutationResponseIdentity(t *testing.T) {
	t.Parallel()
	booking := passbooking.Booking{
		Owner:     "alice",
		Event:     "dance",
		Version:   3,
		CreatedAt: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
	}
	call := ScriptToolRecord{
		Pass: &ScriptPassRequest{ID: "admitted", Command: &passbooking.Command{Event: "dance", Version: 2}},
	}
	trusted, err := registrationReceiptCall(call, booking, nil)
	require.NoError(t, err)
	assert.True(t, registrationResponseMatches(trusted, booking))
	recreated := booking
	recreated.CreatedAt = recreated.CreatedAt.Add(time.Second)
	assert.False(
		t,
		registrationResponseMatches(trusted, recreated),
		"same version does not identify a recreated booking",
	)
	changed := booking
	changed.Version++
	assert.False(t, registrationResponseMatches(trusted, changed))
	trusted.Pass.ID = "another-admission"
	assert.False(t, registrationResponseMatches(trusted, booking), "receipt cannot cross operation identity")
}

func TestRegistrationPayloadOmissionPreservesRetirement(t *testing.T) {
	t.Parallel()
	record := ScriptRecord{
		HistoryRedacted: true,
		PassRedacted:    true,
		MemoryRedacted:  true,
	}
	omitRegistrationPayload(&record)
	assert.True(t, record.HistoryRedacted)
	assert.True(t, record.PassRedacted)
	assert.True(t, record.MemoryRedacted)
}

func TestPaymentCompletionRebasesOnlyExactAttempt(t *testing.T) {
	t.Parallel()
	before := interaction.BookingIdentity{
		Owner:     "alice",
		Event:     "dance",
		Version:   4,
		CreatedAt: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
	}
	after := before
	after.Version++
	prior := passbooking.ReadAuthority{
		Kind:           passbooking.ReadPrivileged,
		Event:          before.Event,
		Owner:          before.Owner,
		Version:        before.Version,
		CreatedAt:      before.CreatedAt,
		Action:         "proof_accept",
		PaymentAttempt: "attempt",
	}
	next := prior
	next.Version++
	next.PaymentAttempt = ""
	receipt := passbooking.PaymentCompletionReceipt{
		Found:    true,
		Before:   prior,
		After:    next,
		Attempt:  "attempt",
		Decision: "accepted",
	}
	foreign := prior
	foreign.PaymentAttempt = "other"
	refs := readsource.Registration(
		[]passbooking.ReadAuthority{prior, foreign, {Kind: passbooking.ReadPaymentRole, Event: "dance"}},
	)
	require.True(t, replaceRegistrationAuthority("bob", 0, refs, before, after, &receipt))
	assert.Equal(t, next, refs[0].Registration)
	assert.Equal(t, foreign, refs[1].Registration)
	assert.Equal(t, passbooking.ReadPaymentRole, refs[2].Registration.Kind)
}

func paymentCompletionFixture() (passbooking.Command, passbooking.PaymentCompletionReceipt) {
	created := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	command := passbooking.Command{
		Name:           PassToolActions()["passes.payments.accept"],
		Event:          "dance",
		Target:         "alice",
		TargetVersion:  4,
		PaymentAttempt: "attempt",
	}
	before := passbooking.ReadAuthority{
		Kind:           passbooking.ReadPrivileged,
		Event:          "dance",
		Owner:          "alice",
		Version:        4,
		CreatedAt:      created,
		Action:         "proof_accept",
		PaymentAttempt: "attempt",
	}
	after := before
	after.Version++
	after.PaymentAttempt = ""
	return command, passbooking.PaymentCompletionReceipt{
		Found:    true,
		Decision: "accepted",
		Attempt:  "attempt",
		Before:   before,
		After:    after,
	}
}

func TestPaymentCompletionRebasesOnlyMatchingQueueAuthorityInDetachedCopy(t *testing.T) {
	t.Parallel()
	_, receipt := paymentCompletionFixture()
	before := interaction.BookingIdentity{
		Owner:     receipt.Before.Owner,
		Event:     receipt.Before.Event,
		Version:   receipt.Before.Version,
		CreatedAt: receipt.Before.CreatedAt,
	}
	after := before
	after.Version++
	foreignAttempt := receipt.Before
	foreignAttempt.PaymentAttempt = "other"
	otherOwner := receipt.Before
	otherOwner.Owner = "carol"
	recreated := receipt.Before
	recreated.CreatedAt = recreated.CreatedAt.Add(time.Second)
	queue := []passbooking.ReadAuthority{receipt.Before, foreignAttempt, otherOwner, recreated}
	record := ScriptRecord{
		HistoryGeneration: 3,
		PassContext: []interaction.PassContextDependency{{
			Request:          agent.RegistrationProposal{Event: "dance", View: hostPaymentQueue},
			QueueAuthorities: queue,
		}},
	}
	original, err := json.Marshal(record)
	require.NoError(t, err)
	clone, err := registrationValidationRecord(record)
	require.NoError(t, err)
	found, err := rebaseRegistrationValidation("bob", &clone, before, after, &receipt)
	require.NoError(t, err)
	require.True(t, found)
	unchanged, err := json.Marshal(record)
	require.NoError(t, err)
	assert.JSONEq(t, string(original), string(unchanged), "the captured record is never mutated")
	assert.Equal(
		t,
		[]passbooking.ReadAuthority{receipt.After, foreignAttempt, otherOwner, recreated},
		clone.PassContext[0].QueueAuthorities,
	)
	assert.Equal(t, record.PassContext[0].Request, clone.PassContext[0].Request, "queue read stays checked")

	withoutPayment, err := registrationValidationRecord(record)
	require.NoError(t, err)
	found, err = rebaseRegistrationValidation("bob", &withoutPayment, before, after, nil)
	require.NoError(t, err)
	assert.False(t, found, "owner-registration completion never rebases payment queue evidence")
	assert.Equal(t, queue, withoutPayment.PassContext[0].QueueAuthorities)
}

func TestPaymentCompletionProbesOnlyExactUncertainAdmission(t *testing.T) {
	t.Parallel()
	command, _ := paymentCompletionFixture()
	generation := int64(0)
	call := func(result string) ScriptToolRecord {
		return ScriptToolRecord{
			Pass:    &ScriptPassRequest{ID: "admitted", Name: "passes.payments.accept", Command: &command},
			Source:  &readsource.Derivation{Generation: &generation, Authorities: []readsource.Authority{}},
			Outcome: agent.ScriptToolResult{Result: json.RawMessage(result)},
		}
	}
	lost := call(`{"operation_id":"admitted","complete":false,"interrupted":true}`)
	assert.True(t, paymentRegistrationCompletion(lost), "a lost reply is probed through the canonical receipt")
	assert.True(t, paymentRegistrationCompletion(call(`{"operation_id":"admitted","complete":true,"result":{}}`)))
	for name, result := range map[string]string{
		"foreign_operation": `{"operation_id":"other","complete":false,"interrupted":true}`,
		"missing_operation": `{"complete":false,"interrupted":true}`,
		"pending":           `{"operation_id":"admitted","complete":false}`,
		"invalid":           `not-json`,
	} {
		assert.False(t, paymentRegistrationCompletion(call(result)), name)
	}
	failed := lost
	failed.Outcome.Error = "forbidden"
	assert.False(t, paymentRegistrationCompletion(failed), "a definite failure is not a committed payment")
	unsourced := lost
	unsourced.Source = nil
	assert.False(t, paymentRegistrationCompletion(unsourced))
	renamed := lost
	renamed.Pass = &ScriptPassRequest{ID: "admitted", Name: "passes.payments.reject", Command: &command}
	assert.False(t, paymentRegistrationCompletion(renamed), "tool and command action must agree")
}

func TestPaymentReceiptMatchesOnlyAdmittedCommand(t *testing.T) {
	t.Parallel()
	command, receipt := paymentCompletionFixture()
	require.True(t, paymentReceiptMatches(command, receipt))
	trusted, err := paymentReceiptCall(ScriptToolRecord{
		Pass: &ScriptPassRequest{ID: "admitted", Name: "passes.payments.accept", Command: &command},
		Outcome: agent.ScriptToolResult{
			Result: json.RawMessage(`{"operation_id":"admitted","complete":false,"interrupted":true}`),
		},
	}, receipt)
	require.NoError(t, err)
	assert.True(t, CommittedPassReceipt(trusted), "the probed receipt replaces the uncertain response")
	assert.JSONEq(
		t,
		`{"operation_id":"admitted","complete":true,"result":{"decision":"accepted"}}`,
		string(trusted.Outcome.Result),
	)
	for name, change := range map[string]func(*passbooking.Command, *passbooking.PaymentCompletionReceipt){
		"missing": func(_ *passbooking.Command, r *passbooking.PaymentCompletionReceipt) { r.Found = false },
		"attempt": func(c *passbooking.Command, _ *passbooking.PaymentCompletionReceipt) { c.PaymentAttempt = "other" },
		"target":  func(c *passbooking.Command, _ *passbooking.PaymentCompletionReceipt) { c.Target = "carol" },
		"event":   func(c *passbooking.Command, _ *passbooking.PaymentCompletionReceipt) { c.Event = "other" },
		"version": func(c *passbooking.Command, _ *passbooking.PaymentCompletionReceipt) { c.TargetVersion++ },
		"skipped": func(_ *passbooking.Command, r *passbooking.PaymentCompletionReceipt) { r.After.Version++ },
		"recreated": func(_ *passbooking.Command, r *passbooking.PaymentCompletionReceipt) {
			r.After.CreatedAt = r.After.CreatedAt.Add(time.Second)
		},
		"pending": func(_ *passbooking.Command, r *passbooking.PaymentCompletionReceipt) {
			r.After.PaymentAttempt = "attempt"
		},
	} {
		changedCommand, changedReceipt := command, receipt
		change(&changedCommand, &changedReceipt)
		assert.False(t, paymentReceiptMatches(changedCommand, changedReceipt), name)
	}
}

func TestPaymentCompletionPreservesCausalAndIncarnationFences(t *testing.T) {
	t.Parallel()
	generation := int64(3)
	before := interaction.BookingIdentity{
		Owner:     "alice",
		Event:     "dance",
		Version:   2,
		CreatedAt: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
	}
	after := before
	after.Version++
	old := passbooking.ReadAuthority{
		Kind:           passbooking.ReadPrivileged,
		Action:         "proof_accept",
		Event:          before.Event,
		Owner:          before.Owner,
		Version:        before.Version,
		CreatedAt:      before.CreatedAt,
		PaymentAttempt: "original",
	}
	receipt := passbooking.PaymentCompletionReceipt{Before: old, After: old}
	receipt.After.Version++
	receipt.After.PaymentAttempt = ""
	for _, scenario := range []string{"own", "foreign", "published", "recreated"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			leaf := old
			causal := readsource.CausalSource{Actor: "bob", Generation: &generation}
			switch scenario {
			case "foreign":
				causal.Actor = "other"
			case "published":
				causal.Published = true
			case "recreated":
				leaf.CreatedAt = leaf.CreatedAt.Add(time.Second)
			}
			causal.Authorities = readsource.Registration([]passbooking.ReadAuthority{leaf})
			refs := []readsource.Authority{{Causal: &causal}}
			changed := replaceRegistrationAuthority("bob", generation, refs, before, after, &receipt)
			require.Equal(t, scenario == "own", changed)
			expected := leaf
			if scenario == "own" {
				expected = receipt.After
			}
			require.Equal(t, expected, refs[0].Causal.Authorities[0].Registration)
		})
	}
}
