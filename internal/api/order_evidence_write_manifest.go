package api

import (
	"github.com/complynx/zns-chatbot/internal/orders"
	"github.com/complynx/zns-chatbot/internal/payments"
)

const orderReviewExpectedRevision = "expected_revision"
const orderReviewPreparationField = "preparation"
const orderReviewLanguageAuto = "auto"
const orderReviewCancelKind = "cancel"

func orderEvidenceMutation(method map[string]any, id string) map[string]any {
	method[manifestEffect], method[manifestIdempotency] = effectWrite, true
	method[manifestStatusPath] = orderEvidencePath + id + "/status"
	method[manifestDedupWindow] = agentRegistrationDedupSeconds
	return method
}

func orderEvidenceSubmitMethod(id map[string]any) map[string]any {
	return orderEvidenceMutation(
		orderEvidenceMethod(
			"submit",
			"Bind your accepted file to your own explicit order/version as reported payment evidence "+
				"after a user request. "+
				"Required event_id/order_id/expected_order_version/file_id/amount_minor. "+
				"Read the frozen order and receipt, verify the amount or clarify it, and clarify "+
				"ambiguous targets. The server chooses orders or meals entitlement from the "+
				"persisted order kind; callers cannot relabel an order. "+
				"Arrival time comes from Telegram metadata. "+
				"Meal proofs sent before the frozen common deadline remain timely if processed or "+
				"reviewed later; service admission also retains its processing cutoff. "+
				"Meal quantities are unlimited and hold no resources. "+
				"Exact timely full-price service claims hold available resources; "+
				"unsupported amount/deadline/resource cases remain needs_decision without holds, and "+
				"exhausted requires refund attention. This is not confirmed payment, fulfillment or "+
				"refund. One non-rejected attempt per order; "+
				"active source files cannot fund another pass/order. "+
				"Reconcile the same operation after uncertainty; status not_found is unknown.",
			map[string]any{
				schemaEventID:              id,
				schemaOrderEvidenceOrderID: id,
				"expected_order_version":   map[string]any{manifestType: schemaInteger, schemaMinimum: 1},
				schemaFileID:               id,
				schemaAmountMinor: map[string]any{
					manifestType:  schemaInteger,
					schemaMinimum: 1,
					schemaMaximum: orders.MaxPrice,
				},
			},
			[]string{
				schemaEventID,
				schemaOrderEvidenceOrderID,
				"expected_order_version",
				schemaFileID,
				schemaAmountMinor,
			},
			[]string{"id", schemaOrderEvidenceOrderID, schemaEventID, schemaState},
		),
		"submit",
	)
}

const (
	orderReviewPrepareDescriptionText = "Save only an explicit human-requested order review intention, without deciding " +
		"money. Read current workspace/evidence and inspect the proof. " +
		"Required full review and expected_revision (0 only if absent); " +
		"optional language auto/en/ru selects the literal approval summary. " +
		"Use the user's message language, with bot/Telegram fallback for language-neutral input. " +
		"An edit or presentation change creates a new immutable revision and invalidates old consent. " +
		"The summary freezes event/order/quote, local deadline, payer, decision/reason and capacity " +
		"effects. Exact full admitted payments can be confirmed; " +
		"unresolved cases can only be rejected here. Complete through review_commit or the same native " +
		"workspace. Retrying returns the original snapshot, not current state."

	orderReviewCommitDescription = "Execute the shared exact order review only after human exact-parameter approval. " +
		"Required workspace_id/revision and unchanged full review. " +
		"Preparation returns the immutable localized description, or a bounded reference summary for " +
		"large presentations. The bot displays the full saved presentation before approval; " +
		"retries never renew expiry. Role/current revision/evidence/proposal/expiry are checked again. " +
		"Native and agent calls share canonical_operation_id and commit decision, capacity changes, " +
		"notices/workspace/source receipt atomically. Completed canonical results recover after expiry " +
		"without a second decision. To change parameters save a new revision and obtain new consent. " +
		"Reconcile this same transport operation after uncertainty; status not_found means unknown."
)

func orderReviewMethods(id map[string]any) []map[string]any {
	review := map[string]any{
		manifestType:               schemaObject,
		schemaAdditionalProperties: false,
		schemaProperties: map[string]any{
			schemaEventID:         id,
			schemaEvidenceID:      id,
			schemaExpectedVersion: map[string]any{manifestType: schemaInteger, schemaEnum: []int{1}},
			paymentSchemaDecision: map[string]any{
				manifestType: schemaString,
				schemaEnum:   []string{"confirmed", "rejected"},
			},
			schemaReason: map[string]any{
				manifestType:    schemaString,
				schemaMinLength: 1,
				schemaMaxLength: payments.MaxReasonLength,
			},
		},
		schemaRequired: []string{
			schemaEventID,
			schemaEvidenceID,
			schemaExpectedVersion,
			paymentSchemaDecision,
			schemaReason,
		},
	}
	revision := map[string]any{manifestType: schemaInteger, schemaMinimum: 1}
	methods := []map[string]any{
		orderEvidenceMethod(
			"review_workspace",
			"Read your current actor-owned order review for event_id/evidence_id before continuing "+
				"from a form or agent. workspace:null means no saved intent, not a payment result. "+
				"Fields include id, revision, state, current and the immutable preparation and a "+
				"bounded description; the bot retains the full consent presentation and displays it "+
				"before approval. A saved-intent receipt can be historical; "+
				"reread current workspace/evidence after replay.",
			map[string]any{schemaEventID: id, schemaEvidenceID: id},
			[]string{schemaEventID, schemaEvidenceID},
			[]string{"workspace"},
		),
		orderEvidenceMethod(
			"review_operation",
			"Read your original shared review transport operation by operation_id. "+
				"receipt:null is unknown. A prepared call is not paid. "+
				"Another approved interface's canonical completion is recovered read-only even after "+
				"expiry; otherwise unavailable_reason identifies "+
				"cancelled/superseded/expired/decided intent. "+
				"Completed save/cancel records intent only, while a commit with review result "+
				"records the decision. Current authority is rechecked.",
			map[string]any{detailsOperationID: id},
			[]string{detailsOperationID},
			[]string{paymentSchemaReceipt},
		),
	}
	save := orderEvidenceMethod(
		"save_review",
		orderReviewPrepareDescriptionText,
		map[string]any{
			paymentSchemaReview:         review,
			orderReviewExpectedRevision: map[string]any{manifestType: schemaInteger, schemaMinimum: 0},
			"language": map[string]any{
				manifestType: schemaString,
				schemaEnum:   []string{orderReviewLanguageAuto, "en", "ru"},
			},
		},
		[]string{paymentSchemaReview, orderReviewExpectedRevision},
		[]string{"id", paymentSchemaRevision, schemaState, orderReviewPreparationField},
	)
	cancel := orderEvidenceMethod(
		"cancel_review",
		"Stop only your current pending review on user cancellation, with workspace_id/revision. "+
			"This invalidates consent; it does not reject evidence, release its holds, cancel an order "+
			"or refund money. No text tags are required. Another revision or completed decision cannot "+
			"be cancelled through the old one.",
		map[string]any{paymentSchemaWorkspaceID: id, paymentSchemaRevision: revision},
		[]string{paymentSchemaWorkspaceID, paymentSchemaRevision},
		[]string{"id", paymentSchemaRevision, schemaState},
	)
	commit := orderEvidenceMethod(
		"review_commit",
		orderReviewCommitDescription,
		map[string]any{paymentSchemaWorkspaceID: id, paymentSchemaRevision: revision, paymentSchemaReview: review},
		[]string{paymentSchemaWorkspaceID, paymentSchemaRevision, paymentSchemaReview},
		[]string{"canonical_operation_id", schemaEvidence},
	)
	commit[manifestConfirmation] = map[string]any{
		confirmationMode:    assignmentConfirmationMode,
		manifestPreparePath: orderEvidencePath + "review_commit/prepare",
	}
	return append(
		methods,
		orderEvidenceMutation(save, "save_review"),
		orderEvidenceMutation(cancel, "cancel_review"),
		orderEvidenceMutation(commit, "review_commit"),
	)
}
