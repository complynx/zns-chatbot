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
	found, err := rebaseRegistrationValidation("alice", &clone, before, after)
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
	trusted, err := registrationReceiptCall(call, booking)
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
