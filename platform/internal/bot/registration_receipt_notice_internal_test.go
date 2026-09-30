package bot

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/botdelivery"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/derivedmutation"
	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	"github.com/complynx/zns-chatbot/platform/internal/interaction"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

type receiptNoticeDomain struct {
	calls []string
	read  func(context.Context, string, derivedmutation.PassOperationInput) (derivedmutation.PassOperationRead, error)
}

func (d *receiptNoticeDomain) ReadPassOperation(
	ctx context.Context, owner string, input derivedmutation.PassOperationInput,
) (derivedmutation.PassOperationRead, error) {
	d.calls = append(d.calls, input.Command.Key)
	return d.read(ctx, owner, input)
}

func receiptNoticeAdmission(id, action string) interaction.RegistrationOperation {
	return interaction.RegistrationOperation{
		ID: id, Tool: "passes.registration." + action, Retired: true,
		Command: &passbooking.Command{Name: action, Event: "dance", Key: id},
		Witness: &passbooking.OperationWitness{Key: id, Digest: "private-digest"},
	}
}

func receiptNoticeStatus(action, status string) derivedmutation.PassOperationRead {
	return derivedmutation.PassOperationRead{
		Summary: derivedmutation.PassOperationSummary{Status: status, Continuation: "unavailable"},
		ReadAuthorities: readsource.Registration([]passbooking.ReadAuthority{
			{Kind: passbooking.ReadCapability, Event: "dance", Action: action},
		}),
	}
}

func TestRegistrationReceiptNoticePartialOutcomes(t *testing.T) {
	t.Parallel()
	for _, language := range []string{"en", "ru"} {
		t.Run(language, func(t *testing.T) {
			t.Parallel()
			admitted := []interaction.RegistrationOperation{
				receiptNoticeAdmission("opaque-first", "cancel"),
				receiptNoticeAdmission("opaque-second", "invite"),
				receiptNoticeAdmission("opaque-denied", "invite"),
			}
			domain := &receiptNoticeDomain{read: func(
				_ context.Context, owner string, input derivedmutation.PassOperationInput,
			) (derivedmutation.PassOperationRead, error) {
				require.Equal(t, "alice", owner)
				require.True(t, input.Retired)
				require.NotNil(t, input.Witness)
				if input.Command.Key == "opaque-denied" {
					return derivedmutation.PassOperationRead{}, &core.ProblemError{Status: http.StatusForbidden}
				}
				status := "committed"
				if input.Command.Name == "invite" {
					status = "not_committed"
				}
				return receiptNoticeStatus(input.Command.Name, status), nil
			}}
			notice, found, err := readRegistrationReceiptNotice(t.Context(), "alice", language, admitted, domain)
			require.NoError(t, err)
			require.True(t, found)
			require.Equal(t, []string{"opaque-first", "opaque-second", "opaque-denied"}, domain.calls)
			cancel, err := i18n.Translate(language, i18n.RegistrationReceiptCancel, nil)
			require.NoError(t, err)
			invite, err := i18n.Translate(language, i18n.RegistrationReceiptInvite, nil)
			require.NoError(t, err)
			committed, err := i18n.Translate(
				language,
				i18n.RegistrationReceiptCommitted,
				map[string]string{"action": cancel},
			)
			require.NoError(t, err)
			missing, err := i18n.Translate(
				language,
				i18n.RegistrationReceiptMissing,
				map[string]string{"action": invite},
			)
			require.NoError(t, err)
			caution, err := i18n.Translate(language, i18n.RegistrationReceiptCaution, nil)
			require.NoError(t, err)
			require.Contains(t, notice.text, committed+"\n"+missing+"\n"+caution)
			require.Equal(t, 1, strings.Count(notice.text, invite))
			for _, secret := range []string{"opaque-", "private-digest", "dance", "alice"} {
				require.NotContains(t, notice.text, secret)
			}
			require.Len(t, notice.ref.Authorities, 2)
			require.Nil(t, notice.ref.Source)
		})
	}
}

func TestRegistrationReceiptNoticeFailureAborts(t *testing.T) {
	t.Parallel()
	for name, failure := range map[string]error{
		"SQL with forbidden": errors.Join(
			&core.ProblemError{Status: http.StatusForbidden}, core.DatabaseOperationError(io.EOF)),
		"SQL with missing": errors.Join(
			&core.ProblemError{Status: http.StatusNotFound}, core.DatabaseOperationError(io.EOF)),
		"cancellation": context.Canceled,
		"provider":     io.ErrUnexpectedEOF,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			admitted := []interaction.RegistrationOperation{
				receiptNoticeAdmission("first", "invite"), receiptNoticeAdmission("second", "invite"),
				receiptNoticeAdmission("third", "invite"),
			}
			domain := &receiptNoticeDomain{read: func(
				_ context.Context, _ string, input derivedmutation.PassOperationInput,
			) (derivedmutation.PassOperationRead, error) {
				if input.Command.Key == "second" {
					return derivedmutation.PassOperationRead{}, failure
				}
				return receiptNoticeStatus("invite", "committed"), nil
			}}
			notice, found, err := readRegistrationReceiptNotice(t.Context(), "alice", "en", admitted, domain)
			require.ErrorIs(t, err, failure)
			require.False(t, found)
			require.Zero(t, notice, "never publish the earlier committed operation after a failed receipt read")
			require.Equal(t, []string{"first", "second"}, domain.calls)
		})
	}
}

func TestRegistrationReceiptNoticeRejectsUnsafeStatus(t *testing.T) {
	t.Parallel()
	for name, mutate := range map[string]func(*derivedmutation.PassOperationRead){
		"context": func(r *derivedmutation.PassOperationRead) {
			r.Summary.Context = &derivedmutation.PassOperationContext{Event: "private"}
		},
		"continuation": func(r *derivedmutation.PassOperationRead) { r.Summary.Continuation = "available" },
		"unknown":      func(r *derivedmutation.PassOperationRead) { r.Summary.Status = "unknown" },
		"no authority": func(r *derivedmutation.PassOperationRead) { r.ReadAuthorities = nil },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			domain := &receiptNoticeDomain{read: func(
				context.Context, string, derivedmutation.PassOperationInput,
			) (derivedmutation.PassOperationRead, error) {
				result := receiptNoticeStatus("invite", "committed")
				mutate(&result)
				return result, nil
			}}
			notice, found, err := readRegistrationReceiptNotice(t.Context(), "alice", "en",
				[]interaction.RegistrationOperation{receiptNoticeAdmission("original", "invite")}, domain)
			require.Error(t, err)
			require.False(t, found)
			require.Zero(t, notice)
		})
	}
}

func TestRegistrationReceiptNoticePreviewOmitsItems(t *testing.T) {
	t.Parallel()
	admitted := []interaction.RegistrationOperation{receiptNoticeAdmission("original", "invite")}
	result := receiptNoticeStatus("invite", "committed")
	domain := &receiptNoticeDomain{read: func(
		context.Context, string, derivedmutation.PassOperationInput,
	) (derivedmutation.PassOperationRead, error) {
		return result, nil
	}}
	clean, found, err := readRegistrationReceiptNotice(t.Context(), "alice", "en", admitted, domain)
	require.NoError(t, err)
	require.True(t, found)
	result.Summary.Items = []passbooking.OperationReceiptItem{{Index: 7, Code: "private-item-canary"}}
	projected, found, err := readRegistrationReceiptNotice(t.Context(), "alice", "en", admitted, domain)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, clean, projected, "the existing preview projection removes item metadata before presentation")
	require.NotContains(t, projected.text, "private-item-canary")
	// A formatter input that bypassed that projection must still fail closed.
	unsafe, err := formatRegistrationReceiptNotice("en", admitted, interaction.RegistrationOperationRead{
		Summaries: []interaction.RegistrationOperationSummary{{
			ID: "original", Tool: admitted[0].Tool, Status: "committed", Continuation: "unavailable",
			Items: result.Summary.Items,
		}},
		ReadAuthorities: result.ReadAuthorities,
	})
	require.Error(t, err)
	require.Zero(t, unsafe)
}

func TestRegistrationReceiptNoticeUnsupportedAdmissionsStayUnavailable(t *testing.T) {
	t.Parallel()
	domain := &receiptNoticeDomain{read: func(
		context.Context, string, derivedmutation.PassOperationInput,
	) (derivedmutation.PassOperationRead, error) {
		t.Fatal("unsupported or executable admission must not reach receipt read")
		return derivedmutation.PassOperationRead{}, nil
	}}
	for _, mutate := range []func(*interaction.RegistrationOperation){
		func(a *interaction.RegistrationOperation) { a.Retired = false },
		func(a *interaction.RegistrationOperation) { a.Tool = "passes.registration.show" },
		func(a *interaction.RegistrationOperation) { a.Tool = "passes.registration.cancel" },
		func(a *interaction.RegistrationOperation) { a.Batch = &passbooking.RuntimeBatch{} },
	} {
		item := receiptNoticeAdmission("original", "invite")
		mutate(&item)
		notice, found, err := readRegistrationReceiptNotice(t.Context(), "alice", "en",
			[]interaction.RegistrationOperation{item}, domain)
		require.NoError(t, err)
		require.False(t, found)
		require.Zero(t, notice)
	}
	require.Empty(t, domain.calls)
}

func TestRegistrationReceiptNoticeDeniedAdmissionsStayUnavailable(t *testing.T) {
	t.Parallel()
	for _, status := range []int{http.StatusForbidden, http.StatusNotFound} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			t.Parallel()
			domain := &receiptNoticeDomain{read: func(
				context.Context, string, derivedmutation.PassOperationInput,
			) (derivedmutation.PassOperationRead, error) {
				return derivedmutation.PassOperationRead{}, &core.ProblemError{Status: status}
			}}
			notice, found, err := readRegistrationReceiptNotice(t.Context(), "alice", "en",
				[]interaction.RegistrationOperation{receiptNoticeAdmission("original", "invite")}, domain)
			require.NoError(t, err)
			require.False(t, found, "ordinary absence must retain the saved unavailable fallback")
			require.Zero(t, notice)
			require.Equal(t, []string{"original"}, domain.calls)
		})
	}
}

func TestRegistrationReceiptNoticeScopeAndEligibility(t *testing.T) {
	t.Parallel()
	ledger := registrationReceiptLedger{
		owner: "alice", items: []interaction.RegistrationOperation{receiptNoticeAdmission("original", "invite")},
	}
	for _, scope := range [][2]string{{"bob", ""}, {"alice", "original"}} {
		result, err := ledger.ReadRegistrationOperations(t.Context(), scope[0], scope[1])
		require.Error(t, err)
		require.Nil(t, result)
	}
	plan := interaction.SavedPlan{
		Kind: interaction.NoticePlan, State: interaction.Ready, SystemNotice: i18n.AgentUnavailable,
	}
	require.True(t, registrationReceiptEligible(plan))
	plan.State = interaction.PrivacyTerminal
	plan.Kind = interaction.TerminalPlan
	plan.TerminalReason = interaction.SourceRevoked
	require.False(t, registrationReceiptEligible(plan))
	generation := int64(0)
	ref := botdelivery.Reference{Family: scriptWorkflowView, CardKey: scriptWorkflowView,
		Generation: &generation, Authorities: receiptNoticeStatus("invite", "committed").ReadAuthorities}
	require.ErrorIs(t, (&Bot{}).bindRegistrationReceiptCard(t.Context(), "alice", &ref, 1), botdelivery.ErrStale)
}

func TestRegistrationReceiptNoticeBindingChanges(t *testing.T) {
	t.Parallel()
	admitted := []interaction.RegistrationOperation{receiptNoticeAdmission("original", "invite")}
	domain := &receiptNoticeDomain{read: func(
		context.Context, string, derivedmutation.PassOperationInput,
	) (derivedmutation.PassOperationRead, error) {
		return receiptNoticeStatus("invite", "committed"), nil
	}}
	first, found, err := readRegistrationReceiptNotice(t.Context(), "alice", "en", admitted, domain)
	require.NoError(t, err)
	require.True(t, found)
	replayed, found, err := readRegistrationReceiptNotice(t.Context(), "alice", "ru", admitted, domain)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, first.ref, replayed.ref, "localization does not change exact receipt identity")
	admitted[0].Witness.Key = "substituted-key"
	changed, found, err := readRegistrationReceiptNotice(t.Context(), "alice", "en", admitted, domain)
	require.NoError(t, err)
	require.True(t, found)
	require.NotEqual(t, first.ref.Object, changed.ref.Object)
	firstHash, err := registrationReceiptViewHash("same-visible-text", first.ref)
	require.NoError(t, err)
	changedHash, err := registrationReceiptViewHash("same-visible-text", changed.ref)
	require.NoError(t, err)
	require.NotEqual(t, firstHash, changedHash, "a different receipt must not reuse the previous message hash")
	replayedHash, err := registrationReceiptViewHash("same-visible-text", replayed.ref)
	require.NoError(t, err)
	require.Equal(t, firstHash, replayedHash)
	capture := &botCardCapture{owner: "alice", reference: first.ref}
	require.ErrorIs(t,
		capture.capture("alice", changed.ref, telegram.Send{}, botdelivery.Continuation{}), botdelivery.ErrStale)
	changed.ref = first.ref
	changed.ref.Authorities = receiptNoticeStatus("cancel", "committed").ReadAuthorities
	require.ErrorIs(t,
		capture.capture("alice", changed.ref, telegram.Send{}, botdelivery.Continuation{}), botdelivery.ErrStale)
	changed.ref = first.ref
	changed.ref.Update++
	require.ErrorIs(t,
		capture.capture("alice", changed.ref, telegram.Send{}, botdelivery.Continuation{}), botdelivery.ErrStale)
}
