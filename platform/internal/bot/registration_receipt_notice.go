package bot

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/agenthost"
	"github.com/complynx/zns-chatbot/platform/internal/botdelivery"
	"github.com/complynx/zns-chatbot/platform/internal/derivedmutation"
	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	"github.com/complynx/zns-chatbot/platform/internal/interaction"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

const registrationReceiptObject = "registration-receipts:"

// This ledger cannot substitute an admission from another update or owner.
type registrationReceiptLedger struct {
	owner string
	items []interaction.RegistrationOperation
}

func (s registrationReceiptLedger) ReadRegistrationOperations(
	ctx context.Context, owner, id string,
) ([]interaction.RegistrationOperation, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if owner != s.owner || id != "" {
		return nil, errors.New("invalid receipt presentation scope")
	}
	return s.items, nil
}

type registrationReceiptNotice struct {
	text string
	ref  botdelivery.Reference
}

type registrationReceiptIdentity struct {
	ID      string                        `json:"id"`
	Tool    string                        `json:"tool"`
	Witness *passbooking.OperationWitness `json:"witness"`
	Status  string                        `json:"status"`
}

func registrationReceiptEligible(plan interaction.SavedPlan) bool {
	return plan.Kind == interaction.NoticePlan && plan.State == interaction.Ready &&
		plan.TerminalReason == "" && plan.SystemNotice == i18n.AgentUnavailable
}

// Equal prose for a different receipt must still pass the delivery boundary.
func registrationReceiptViewHash(payloadHash string, ref botdelivery.Reference) (string, error) {
	raw, err := json.Marshal(struct {
		Payload string                `json:"payload"`
		Binding botdelivery.Reference `json:"binding"`
	}{Payload: payloadHash, Binding: ref})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:]), nil
}

func (b *Bot) bindRegistrationReceiptCard(
	ctx context.Context, owner string, ref *botdelivery.Reference, generation int64,
) error {
	if ref.Family != scriptWorkflowView || ref.CardKey != scriptWorkflowView || ref.Generation == nil ||
		*ref.Generation != generation || ref.Source != nil || len(ref.Authorities) == 0 {
		return botdelivery.ErrStale
	}
	latest, err := (interaction.Store{DB: b.DB}).LatestNotice(ctx, owner)
	if errors.Is(err, pgx.ErrNoRows) {
		return botdelivery.ErrStale
	}
	if err != nil {
		return err
	}
	if latest.UpdateID != ref.Update || latest.SourceRevoked || latest.System != i18n.AgentUnavailable {
		return botdelivery.ErrStale
	}
	plan, err := (interaction.Store{DB: b.DB}).Load(ctx, owner, ref.Update)
	if err != nil {
		return err
	}
	if !registrationReceiptEligible(plan) || plan.HistoryGeneration != generation {
		return botdelivery.ErrStale
	}
	return nil
}

func (b *Bot) registrationReceiptNotice(
	ctx context.Context, owner, language string, update int64,
) (registrationReceiptNotice, bool, error) {
	plan, err := (interaction.Store{DB: b.DB}).Load(ctx, owner, update)
	if err != nil || !registrationReceiptEligible(plan) {
		return registrationReceiptNotice{}, false, err
	}
	generation, err := b.API.HistoryGeneration(ctx, owner)
	if err != nil || generation != plan.HistoryGeneration {
		return registrationReceiptNotice{}, false, err
	}
	admitted, err := (agenthost.ScriptStore{DB: b.DB}).ReadRegistrationOperationsForUpdate(ctx, owner, update)
	if err != nil {
		return registrationReceiptNotice{}, false, err
	}
	notice, found, err := readRegistrationReceiptNotice(ctx, owner, language, admitted, b.Host)
	if err != nil || !found {
		return notice, found, err
	}
	notice.ref.Update, notice.ref.Generation = update, &generation
	return notice, true, nil
}

// Only currently authorized, body-free canonical status may supplement the
// immutable unavailable winner. It never resumes a command or reads its result.
func readRegistrationReceiptNotice(
	ctx context.Context, owner, language string, admitted []interaction.RegistrationOperation,
	domain interaction.RegistrationOperationDomain,
) (registrationReceiptNotice, bool, error) {
	if len(admitted) > agent.MaxScriptRuns*agenthost.MaxScriptCalls {
		return registrationReceiptNotice{}, false, errors.New("receipt presentation exceeds admitted call bound")
	}
	selected := make([]interaction.RegistrationOperation, 0, len(admitted))
	seen := make(map[string]bool, len(admitted))
	for _, item := range admitted {
		if item.ID == "" || seen[item.ID] {
			return registrationReceiptNotice{}, false, errors.New("invalid receipt presentation identity")
		}
		seen[item.ID] = true
		if item.Retired && item.Command != nil && item.Assignment == nil && item.Batch == nil && item.Menu == nil &&
			registrationReceiptAction(item.Tool) != "" && agenthost.PassToolActions()[item.Tool] == item.Command.Name {
			selected = append(selected, item)
		}
	}
	if len(selected) == 0 {
		return registrationReceiptNotice{}, false, nil
	}
	reader := interaction.RegistrationOperations{
		Ledger: registrationReceiptLedger{owner: owner, items: selected}, Domain: domain,
	}
	result, err := reader.Read(ctx, owner, derivedmutation.PassOperationQuery{})
	if err != nil {
		return registrationReceiptNotice{}, false, err
	}
	if len(result.Summaries) == 0 {
		return registrationReceiptNotice{}, false, nil
	}
	if len(result.ReadAuthorities) == 0 || !readsource.Valid(result.ReadAuthorities) {
		return registrationReceiptNotice{}, false, errors.New("missing receipt presentation authority")
	}
	notice, err := formatRegistrationReceiptNotice(language, selected, result)
	return notice, err == nil, err
}

func formatRegistrationReceiptNotice(
	language string, selected []interaction.RegistrationOperation, result interaction.RegistrationOperationRead,
) (registrationReceiptNotice, error) {
	m := &orderMessages{language: language}
	lines := []string{m.text(i18n.RegistrationReceiptTitle, nil)}
	identities := make([]registrationReceiptIdentity, 0, len(selected))
	for _, item := range selected {
		identity := registrationReceiptIdentity{ID: item.ID, Tool: item.Tool, Witness: item.Witness}
		for _, summary := range result.Summaries {
			if summary.ID != item.ID {
				continue
			}
			if summary.Tool != item.Tool || summary.Context != nil || summary.Continuation != "unavailable" ||
				len(summary.Items) != 0 || (summary.Status != "committed" && summary.Status != "not_committed") {
				return registrationReceiptNotice{}, errors.New("invalid receipt presentation status")
			}
			identity.Status = summary.Status
			status := i18n.RegistrationReceiptCommitted
			if summary.Status == "not_committed" {
				status = i18n.RegistrationReceiptMissing
			}
			lines = append(
				lines,
				m.text(status, map[string]string{"action": m.text(registrationReceiptAction(item.Tool), nil)}),
			)
		}
		identities = append(identities, identity)
	}
	lines = append(lines, m.text(i18n.RegistrationReceiptCaution, nil))
	if m.err != nil {
		return registrationReceiptNotice{}, m.err
	}
	raw, err := json.Marshal(identities)
	if err != nil {
		return registrationReceiptNotice{}, err
	}
	digest := sha256.Sum256(raw)
	return registrationReceiptNotice{
		text: strings.Join(lines, "\n"),
		ref: botdelivery.Reference{
			Family: scriptWorkflowView, CardKey: scriptWorkflowView,
			Object:      registrationReceiptObject + hex.EncodeToString(digest[:]),
			Authorities: readsource.CloneAuthorities(result.ReadAuthorities),
		},
	}, nil
}

func registrationReceiptAction(tool string) i18n.ID {
	return map[string]i18n.ID{
		"passes.registration.solo":          i18n.RegistrationReceiptSolo,
		"passes.registration.invite":        i18n.RegistrationReceiptInvite,
		"passes.registration.accept":        i18n.RegistrationReceiptAccept,
		"passes.registration.decline":       i18n.RegistrationReceiptDecline,
		"passes.registration.cancel":        i18n.RegistrationReceiptCancel,
		"passes.registration.payment_admin": i18n.RegistrationReceiptPaymentAdmin,
		"passes.admin.cancel":               i18n.RegistrationReceiptAdminCancel,
		"passes.admin.uncouple":             i18n.RegistrationReceiptUncouple,
		"passes.admin.recalculate":          i18n.RegistrationReceiptRecalculate,
		"passes.payments.accept":            i18n.RegistrationReceiptPaymentAccept,
		"passes.payments.reject":            i18n.RegistrationReceiptPaymentReject,
		"passes.takeover.apply":             i18n.RegistrationReceiptTakeover,
		"passes.takeover.received_only":     i18n.RegistrationReceiptReceived,
	}[tool]
}
