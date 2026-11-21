package api

import (
	"slices"
	"strings"

	"github.com/complynx/zns-chatbot/internal/db"
)

const externalAgentMark = "external_agent"

const (
	administrationModuleID = "zinusya_administration"
	knowledgeModuleID      = "zinusya_knowledge"
	paymentsModuleID       = "zinusya_payments"
	orderPaymentsModuleID  = "zinusya_order_payments"
	pairsModuleID          = "zinusya_pairs"
)

type externalAgentReadKey struct{}

// Review new methods before adding them. File transfers through Loom's run
// artifact store and access management are outside the external catalog.
const externalAgentCatalog = `
zinusya_account:
me

zinusya_authorities:
me

zinusya_profile:
get operation

zinusya_people:
search info list resolve

zinusya_communications:
status audience

zinusya_history:
page text registration registrations avatar_candidates avatar_info

zinusya_avatars:
get operation draft drafts draft_operation finalization_operation finalized finalized_list

zinusya_context:
resolve list

zinusya_files:
info

zinusya_orders:
catalog quote events selection selection_operation list get operation

zinusya_meals:
catalog draft draft_operation list get operation

zinusya_massage:
schedule candidate_starts get list operation pending availability

zinusya_massage_staff:
events schedule windows timetable client preferences

zinusya_registrations:
me list invitation

zinusya_registration_details:
get operation readiness request request_operation requests

zinusya_knowledge:
notifications overview list search read outline find revisions suggestion my_suggestions
capabilities operation recent_actions pending_suggestions moderators

zinusya_payments:
expectations operation events balance evidence list queue review_workspace review_operation

zinusya_order_payments:
evidence list operation evidence_for_review queue review_workspace review_operation

zinusya_pairs:
my partner_code invitation_context invitations events candidates assignment_proposal assignment_proposals

zinusya_administration:
assignment_proposal assignment_proposals events event registrations authorities notifications notification
pair_notifications pair_notification order_notifications order_notification knowledge_notification
massage_reminders massage_reminder massage_specialist_notifications massage_specialist_notification
user user_revocation_receipt orders_catalog meal_catalog massage_schedule massage_bookings validate_archive
validate_stored_archive settings tasks task avatar_delivery avatar_delivery_proposal avatar_delivery_pending
massage_specialist_events massage_specialist_bindings massage_specialist_binding massage_specialist_target
massage_specialist_pending massage_specialist_proposal

`

func markExternalAgentMethods(module string, methods []map[string]any) {
	_, section, found := strings.Cut(externalAgentCatalog, "\n"+module+":\n")
	if !found {
		return
	}
	list, _, _ := strings.Cut(section, "\n\n")
	ids := strings.Fields(list)
	for _, method := range methods {
		id, _ := method["id"].(string)
		mimes, _ := method[manifestMIMETypes].([]string)
		if method[manifestEffect] == effectRead && len(mimes) == 0 && slices.Contains(ids, id) {
			method[externalAgentMark] = true
		}
	}
}

func markExternalAgentModules(modules []any, writes bool) {
	for _, value := range modules {
		module, _ := value.(map[string]any)
		id, _ := module["id"].(string)
		methods := manifestMethodCards(module)
		markExternalAgentMethods(id, methods)
		if writes {
			markExternalAgentWriteMethods(id, methods)
		}
	}
}

func manifestMethodCards(module map[string]any) []map[string]any {
	switch methods := module[manifestMethods].(type) {
	case []map[string]any:
		return methods
	case []any:
		cards := make([]map[string]any, 0, len(methods))
		for _, value := range methods {
			if method, ok := value.(map[string]any); ok {
				cards = append(cards, method)
			}
		}
		return cards
	default:
		return nil
	}
}

// Capture the maximum path set at startup. Role and ownership checks still
// run in each discovery or tool handler with the user's current principal.
func externalAgentReadPaths(modules []any) map[string]bool {
	paths := make(map[string]bool)
	for _, value := range modules {
		module, _ := value.(map[string]any)
		id, _ := module["id"].(string)
		methods := manifestMethodCards(module)
		if discover, ok := module[manifestDiscoveryPath].(string); ok {
			paths[discover] = true
			methods = externalAgentDiscoveryCards(id)
		}
		markExternalAgentMethods(id, methods)
		for _, method := range methods {
			path, _ := method[manifestPath].(string)
			if method[externalAgentMark] == true && path != "" {
				paths[path] = true
			}
		}
	}
	return paths
}

func externalAgentDiscoveryCards(module string) []map[string]any {
	switch module {
	case administrationModuleID:
		return administrativeMethods()
	case knowledgeModuleID:
		return knowledgeMethods(db.KnowledgeCapabilities{CanEdit: true, GlobalAdministrator: true})
	case paymentsModuleID:
		return paymentMethods(true)
	case orderPaymentsModuleID:
		return orderEvidenceMethods(true)
	case pairsModuleID:
		methods := append(pairParticipantMethods(), pairInvitationMethods()...)
		methods = append(methods, pairPrerequisiteMethods()...)
		methods = append(methods, pairAssignmentProposalMethods()...)
		return append(methods, pairAssignmentMethod())
	default:
		return nil
	}
}
