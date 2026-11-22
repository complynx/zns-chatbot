package api

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"slices"
	"strings"

	"github.com/complynx/zns-chatbot/internal/identity"
	"github.com/complynx/zns-chatbot/internal/loom"
)

const externalAgentApprovalPath = "/internal/agent/external-agent/approval"
const externalWriteHeader = "X-Loom-External-Write-Key"
const externalApprovalBodyLimit = 128 << 10

// These are user-facing actions with a stable operation receipt. Run-local
// onboarding, context selection and artifact operations stay internal.
const externalAgentWriteCatalog = `
zinusya_profile:
set_name set_language set_visibility set_gender

zinusya_knowledge:
suggest edit review set_moderator

zinusya_communications:
prepare

zinusya_avatars:
create_draft save_draft finalize_draft

zinusya_payments:
submit review save_review cancel_review review_commit

zinusya_order_payments:
submit save_review cancel_review review_commit

zinusya_pairs:
dissolve invite accept decline revoke assign

zinusya_orders:
save_selection submit cancel

zinusya_meals:
save_draft submit cancel

zinusya_massage:
book cancel

zinusya_massage_staff:
instant set_preferences

zinusya_registrations:
join

zinusya_registration_details:
set_legal_name set_passport_number begin_request

zinusya_administration:
assign authority resolve_notification revoke_user close_task update_settings change_event
set_orders_catalog set_meal_catalog set_massage_schedule store_archive apply_history
resolve_avatar_delivery set_massage_specialist_binding

`

func markExternalAgentWriteMethods(module string, methods []map[string]any) {
	_, section, found := strings.Cut(externalAgentWriteCatalog, "\n"+module+":\n")
	if !found {
		return
	}
	list, _, _ := strings.Cut(section, "\n\n")
	ids := strings.Fields(list)
	for _, method := range methods {
		id, _ := method["id"].(string)
		status, _ := method[manifestStatusPath].(string)
		mimes, _ := method[manifestMIMETypes].([]string)
		if method[manifestEffect] != effectWrite || status == "" || method[manifestIdempotency] != true ||
			len(mimes) != 0 || !slices.Contains(ids, id) {
			continue
		}
		method[externalAgentMark] = true
		if rememberExternalApproval(module, id) {
			method["external_agent_remember"] = true
		}
		if module == "zinusya_registration_details" && id == "set_passport_number" {
			method["external_agent_protected_arguments"] = true
		}
	}
}

func rememberExternalApproval(module, method string) bool {
	return module == knowledgeModuleID && slices.Contains(strings.Fields("suggest edit"), method) ||
		module == "zinusya_massage" && slices.Contains(strings.Fields("book cancel"), method)
}

func externalAgentWritePaths(modules []any) map[string]bool {
	paths := make(map[string]bool)
	for _, value := range modules {
		module, _ := value.(map[string]any)
		id, _ := module["id"].(string)
		methods := manifestMethodCards(module)
		if module[manifestDiscovery] == discoveryDynamic {
			methods = externalAgentDiscoveryCards(id)
		}
		markExternalAgentWriteMethods(id, methods)
		for _, method := range methods {
			if method[externalAgentMark] != true {
				continue
			}
			for _, key := range []string{manifestPath, manifestStatusPath} {
				if path, ok := method[key].(string); ok && path != "" {
					paths[path] = true
				}
			}
			confirmation, _ := method[manifestConfirmation].(map[string]any)
			if path, ok := confirmation[manifestPreparePath].(string); ok && path != "" {
				paths[path] = true
			}
		}
	}
	return paths
}

func (s *Server) externalWriteAuthorized(r *http.Request) bool {
	if !s.externalWritePaths[r.URL.Path] {
		return false
	}
	return externalAgentSecretMatches(s.options.ExternalAgentTokenSecret, r.Header.Get(externalWriteHeader))
}

func externalAgentSecretMatches(secret, candidate string) bool {
	expected, provided := sha256.Sum256([]byte(secret)), sha256.Sum256([]byte(candidate))
	return secret != "" && subtle.ConstantTimeCompare(expected[:], provided[:]) == 1
}

type externalApprovalDelivery interface {
	DeliverExternalApproval(context.Context, loom.ExternalApproval) error
}

func (s *Server) receiveExternalApproval(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	bearer, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok || !externalAgentSecretMatches(s.options.ExternalAgentTokenSecret, bearer) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	delivery, ok := s.tasks.(externalApprovalDelivery)
	if !ok {
		http.Error(w, "approval delivery unavailable", http.StatusServiceUnavailable)
		return
	}
	body := json.NewDecoder(http.MaxBytesReader(w, r.Body, externalApprovalBodyLimit))
	body.DisallowUnknownFields()
	var approval loom.ExternalApproval
	if err := body.Decode(&approval); err != nil || !errors.Is(body.Decode(new(any)), io.EOF) ||
		approval.Service == "" || approval.Module == "" || approval.Method == "" || approval.Description == "" {
		http.Error(w, "invalid approval", http.StatusBadRequest)
		return
	}
	if err := delivery.DeliverExternalApproval(r.Context(), approval); err != nil {
		if errors.Is(err, identity.ErrDenied) {
			http.Error(w, "approval recipient denied", http.StatusForbidden)
			return
		}
		http.Error(w, "approval delivery unavailable", http.StatusServiceUnavailable)
		return
	}
	writeJSON(w, http.StatusOK, struct{}{})
}
