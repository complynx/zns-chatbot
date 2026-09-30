package agenthost

import (
	"bytes"
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/conversation"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/interaction"
	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
	knowledgeauthority "github.com/complynx/zns-chatbot/platform/internal/knowledge/authority"
	"github.com/complynx/zns-chatbot/platform/internal/legacyfood"
	"github.com/complynx/zns-chatbot/platform/internal/massage"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

func MergeReadAuthorities(groups ...[]readsource.Authority) ([]readsource.Authority, error) {
	return readsource.Merge(groups...)
}

func BookingReadAuthority(booking interaction.BookingIdentity) passbooking.ReadAuthority {
	return passbooking.ReadAuthority{Kind: passbooking.ReadOwnerBooking, Event: booking.Event,
		Owner: booking.Owner, Version: booking.Version, CreatedAt: booking.CreatedAt}
}

func ScriptReadAuthorities(owner string, records []ScriptRecord) ([]readsource.Authority, error) {
	result := []readsource.Authority{}
	for _, record := range records {
		if record.PassRedacted || record.HistoryRedacted || record.MemoryRedacted {
			continue
		}
		direct, err := PassContextReadAuthorities(record.PassContext)
		if err != nil {
			return nil, err
		}
		result, err = MergeReadAuthorities(result, direct, record.ReadAuthorities)
		if err != nil {
			return nil, err
		}
		for _, call := range record.Calls {
			evidence, callErr := ScriptCallReadAuthorities(owner, call)
			if callErr != nil {
				return nil, callErr
			}
			result, err = MergeReadAuthorities(result, evidence)
			if err != nil {
				return nil, err
			}
		}
	}
	return result, nil
}

func ScriptCallReadAuthorities(owner string, call ScriptToolRecord) ([]readsource.Authority, error) {
	refs, err := ScriptCallResultAuthorities(call)
	if err != nil || call.Source == nil || call.Outcome.Error != "" || CommittedPassReceipt(call) {
		return refs, err
	}
	inherited, err := readsource.Capture(owner, *call.Source)
	if err != nil {
		return nil, err
	}
	return readsource.Merge(refs, inherited)
}

func CommittedPassReceipt(call ScriptToolRecord) bool {
	request := call.Pass
	if request == nil || request.Menu != nil || request.Batch != nil || request.Name == hostPassesExport ||
		(request.Command == nil && request.Assignment == nil) {
		return false
	}
	var receipt struct {
		Complete bool            `json:"complete"`
		Result   json.RawMessage `json:"result"`
	}
	return json.Unmarshal(call.Outcome.Result, &receipt) == nil && receipt.Complete && len(receipt.Result) > 0 &&
		string(receipt.Result) != "null"
}

func ScriptCallResultAuthorities(call ScriptToolRecord) ([]readsource.Authority, error) {
	if call.Outcome.Error != "" {
		return nil, nil
	}
	if refs, required, err := ScriptPrivilegedResultAuthorities(call); required {
		return refs, err
	}
	if len(call.Outcome.Result) == 0 {
		return nil, nil
	}
	if call.ResultAuthorities != nil {
		return MergeReadAuthorities(call.ResultAuthorities)
	}
	if call.Outcome.Name == "passes.operations" {
		var result interaction.RegistrationOperationRead
		if err := json.Unmarshal(call.Outcome.Result, &result); err != nil {
			return nil, err
		}
		return MergeReadAuthorities(result.ReadAuthorities)
	}
	if call.PassReceiptID != "" {
		var result interaction.RegistrationReceiptObservation
		if err := json.Unmarshal(call.Outcome.Result, &result); err != nil {
			return nil, err
		}
		if result.ID != call.PassReceiptID || !result.Complete || result.ReadAuthorities == nil {
			return nil, errors.New("pass receipt read authority missing")
		}
		return MergeReadAuthorities(result.ReadAuthorities)
	}
	if call.Memory != nil {
		return ScriptMemoryResultAuthorities(call.Outcome.Result)
	}
	if call.MemoryReadState != nil {
		refs, err := ScriptMemoryReadAuthorities(call)
		if err != nil || len(refs) > 0 {
			return refs, err
		}
	}
	if call.KnowledgeRead != nil {
		return []readsource.Authority{{Knowledge: *call.KnowledgeRead}}, nil
	}
	return ScriptRegistrationAndHistoryAuthorities(call)
}

func ScriptRegistrationAndHistoryAuthorities(call ScriptToolRecord) ([]readsource.Authority, error) {
	if _, privileged := PassPrivilegedReadView(
		call.Outcome.Name,
	); privileged ||
		call.Outcome.Name == "passes.registration.read" {
		var read agent.RegistrationReadResult
		if err := json.Unmarshal(call.Outcome.Result, &read); err != nil {
			return nil, err
		}
		return PassContextReadAuthorities(
			ScriptPassContext(&agent.RegistrationContext{Reads: []agent.RegistrationReadResult{read}}),
		)
	}
	switch call.Outcome.Name {
	case "passes.get":
		var booking passbooking.Booking
		if err := json.Unmarshal(call.Outcome.Result, &booking); err != nil {
			return nil, err
		}
		if booking.Version == 0 {
			return nil, nil
		}
		return readsource.Registration([]passbooking.ReadAuthority{BookingReadAuthority(PassIdentity(booking))}), nil
	case hostPassesEvents:
		return ScriptEventReadAuthorities(call)
	case hostPassesInvitations:
		return ScriptInvitationReadAuthorities(call)
	case hostPassesTiers:
		if call.PassRead == nil {
			return nil, errors.New("registration read provenance missing")
		}
		return readsource.Registration([]passbooking.ReadAuthority{
			{
				Kind:   passbooking.ReadCapability,
				Event:  call.PassRead.Event,
				Action: PassToolActions()[call.Outcome.Name],
			},
		}), nil
	case hostHistoryRead:
		var chunk conversation.TextChunk
		if err := json.Unmarshal(call.Outcome.Result, &chunk); err != nil {
			return nil, err
		}
		return chunk.ReadAuthorities, nil
	case hostHistoryPage:
		var page conversation.Page
		if err := json.Unmarshal(call.Outcome.Result, &page); err != nil {
			return nil, err
		}
		return HistoryPageReadAuthorities(page)
	default:
		return ScriptEffectReadAuthorities(call)
	}
}

func ScriptInvitationReadAuthorities(call ScriptToolRecord) ([]readsource.Authority, error) {
	if call.PassRead == nil {
		return nil, errors.New("registration read provenance missing")
	}
	var page ScriptDomainPage[passbooking.Invitation]
	if err := json.Unmarshal(call.Outcome.Result, &page); err != nil {
		return nil, err
	}
	result := []readsource.Authority{}
	for _, invite := range page.Items {
		result = append(
			result,
			readsource.Authority{
				Registration: passbooking.ReadAuthority{Kind: passbooking.ReadInvitation, Event: call.PassRead.Event,
					Owner: invite.From.Owner, Version: invite.Version, CreatedAt: invite.CreatedAt},
			},
		)
	}
	return result, nil
}

func ScriptEffectReadAuthorities(call ScriptToolRecord) ([]readsource.Authority, error) {
	request := call.Pass
	if request == nil || request.Menu != nil || request.Batch != nil || request.Name == hostPassesExport {
		return nil, nil
	}
	var receipt struct {
		Complete bool            `json:"complete"`
		Result   json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(call.Outcome.Result, &receipt); err != nil {
		return nil, err
	}
	if !receipt.Complete || len(receipt.Result) == 0 {
		return nil, nil
	}
	if request.Command != nil && strings.HasPrefix(request.Name, "passes.registration.") {
		var booking passbooking.Booking
		if err := json.Unmarshal(receipt.Result, &booking); err != nil {
			return nil, err
		}
		return readsource.Registration([]passbooking.ReadAuthority{BookingReadAuthority(PassIdentity(booking))}), nil
	}
	authority := passbooking.ReadAuthority{Kind: passbooking.ReadCapability, Action: PassToolActions()[request.Name]}
	switch {
	case request.Command != nil:
		authority.Event = request.Command.Event
	case request.Assignment != nil:
		authority.Event = request.Assignment.Event
	default:
		return nil, errors.New("registration effect provenance missing")
	}
	return readsource.Registration([]passbooking.ReadAuthority{authority}), nil
}

func HistoryPageReadAuthorities(page conversation.Page) ([]readsource.Authority, error) {
	result, err := MergeReadAuthorities(page.ReadAuthorities)
	if err != nil {
		return nil, err
	}
	for _, event := range page.Events {
		result, err = MergeReadAuthorities(result, event.ReadAuthorities)
		if err != nil {
			return nil, err
		}
	}
	return result, nil
}

func HistoryInputReadAuthorities(input *agent.Input) ([]readsource.Authority, error) {
	result := append([]readsource.Authority{}, input.ReadAuthorities...)
	if input.Conversation == nil {
		return result, nil
	}
	var err error
	result, err = MergeReadAuthorities(result, input.Conversation.Summary.ReadAuthorities)
	if err != nil {
		return nil, err
	}
	for _, page := range input.Conversation.Reads {
		refs, pageErr := HistoryPageReadAuthorities(page)
		if pageErr != nil {
			return nil, pageErr
		}
		result, err = MergeReadAuthorities(result, refs)
		if err != nil {
			return nil, err
		}
	}
	return result, nil
}

func SetModelHistoryPages(input *agent.Input, pages []conversation.Page) error {
	result := append([]conversation.Page{}, pages...)
	for index := range result {
		refs, err := HistoryPageReadAuthorities(result[index])
		if err != nil {
			return err
		}
		input.ReadAuthorities, err = MergeReadAuthorities(input.ReadAuthorities, refs)
		if err != nil {
			return err
		}
		result[index].ReadAuthorities = nil
		result[index].Events = append([]conversation.Event{}, result[index].Events...)
		for event := range result[index].Events {
			result[index].Events[event].ReadAuthorities = nil
		}
	}
	input.Conversation.Reads = result
	input.Conversation.Summary.ReadAuthorities = nil
	return nil
}

func ScriptEventReadAuthorities(call ScriptToolRecord) ([]readsource.Authority, error) {
	var page core.ReadPage[passbooking.NavigationEvent]
	if err := json.Unmarshal(call.Outcome.Result, &page); err != nil {
		return nil, err
	}
	result := []readsource.Authority{}
	for _, item := range page.Items {
		if item.Access == passbooking.NavigationOwned {
			result = append(
				result,
				readsource.Authority{
					Registration: passbooking.ReadAuthority{Kind: passbooking.ReadOwnedEvent, Event: item.ID},
				},
			)
		}
	}
	return result, nil
}

func RegistrationContextReadAuthorities(
	dependencies []interaction.PassContextDependency,
) ([]passbooking.ReadAuthority, error) {
	result := []passbooking.ReadAuthority{}
	for _, dependency := range dependencies {
		result = append(result, dependency.QueueAuthorities...)
		if dependency.Booking != nil {
			result = append(result, BookingReadAuthority(*dependency.Booking))
		}
		request := dependency.Request
		authority := passbooking.ReadAuthority{Kind: passbooking.ReadPrivileged, Event: request.Event}
		switch request.View {
		case hostQueue:
			authority.Action = agent.RegistrationAdminAssign
		case hostPaymentQueue:
			authority.Action = hostProofAccept
		case agent.RegistrationAdminTarget, agent.RegistrationTakeoverTarget:
			target, err := strconv.ParseInt(request.Target, 10, 64)
			if err != nil {
				return nil, err
			}
			authority.TargetTelegramID = target
			authority.Action = agent.RegistrationAdminAssign
			if request.View == agent.RegistrationTakeoverTarget {
				authority.Action = passbooking.CommandTakeover
			}
		case "invitations":
			for _, invite := range dependency.Invitations {
				result = append(
					result,
					passbooking.ReadAuthority{Kind: passbooking.ReadInvitation, Event: request.Event,
						Owner: invite.Owner, Version: invite.Version, CreatedAt: invite.CreatedAt},
				)
			}
		}
		if authority.Action != "" {
			if dependency.TargetBooking != nil {
				authority.Owner = dependency.TargetBooking.Owner
				authority.Version = dependency.TargetBooking.Version
				authority.CreatedAt = dependency.TargetBooking.CreatedAt
			}
			result = append(result, authority)
		}
	}
	return result, nil
}

func PassContextReadAuthorities(dependencies []interaction.PassContextDependency) ([]readsource.Authority, error) {
	refs, err := RegistrationContextReadAuthorities(dependencies)
	if err != nil {
		return nil, err
	}
	return MergeReadAuthorities(readsource.Registration(refs))
}

func KnowledgeReadAuthorities(input *agent.KnowledgeContext) []readsource.Authority {
	refs := []readsource.Authority{}
	if input == nil {
		return refs
	}
	for _, memo := range input.Memos {
		refs = append(refs, memo.ReadAuthorities...)
	}
	private := len(input.Memos) > 0
	shared := false
	if input.Memory != nil {
		refs = append(refs, input.Memory.Overview.ReadAuthorities...)
		for _, entry := range input.Memory.Overview.Summaries {
			refs = append(refs, entry.ReadAuthorities...)
			private = private || entry.Namespace == knowledge.MemoryPrivate
			shared = shared || entry.Namespace == knowledge.MemoryShared
		}
	}
	for _, read := range input.Reads {
		if read.Error != "" {
			continue
		}
		refs = append(refs, KnowledgeReadEvidence(read)...)
		private = private || read.Memo != nil
		shared = shared || len(read.Facts) > 0
	}
	if private && input.ReadState != nil {
		refs = append(
			refs,
			readsource.Authority{
				Knowledge: knowledgeauthority.ReadAuthority{
					Kind:       knowledgeauthority.PrivateMemory,
					Generation: input.ReadState.PrivateGeneration,
				},
			},
		)
	}
	if shared && input.ReadState != nil {
		refs = append(
			refs,
			readsource.Authority{
				Knowledge: knowledgeauthority.ReadAuthority{
					Kind:       knowledgeauthority.SharedMemory,
					Generation: input.ReadState.SharedGeneration,
				},
			},
		)
	}
	return refs
}

func ScriptMemoryResultAuthorities(raw json.RawMessage) ([]readsource.Authority, error) {
	var result knowledge.Result
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, err
	}
	groups := [][]readsource.Authority{result.ReadAuthorities}
	if result.Fact != nil {
		groups = append(groups, result.Fact.ReadAuthorities)
	}
	if result.Memo != nil {
		groups = append(groups, result.Memo.ReadAuthorities)
	}
	if result.Document != nil {
		groups = append(groups, result.Document.ReadAuthorities)
	}
	if result.Proposal != nil {
		groups = append(groups, result.Proposal.ReadAuthorities)
	}
	return MergeReadAuthorities(groups...)
}

func KnowledgeReadEvidence(read agent.KnowledgeReadResult) []readsource.Authority {
	refs := []readsource.Authority{}
	if read.Memo != nil {
		refs = append(refs, read.Memo.ReadAuthorities...)
	}
	for _, fact := range read.Facts {
		refs = append(refs, fact.ReadAuthorities...)
	}
	for _, proposal := range read.Proposals {
		refs = append(refs, proposal.ReadAuthorities...)
	}
	if read.Request.ReviewQueue {
		refs = append(
			refs,
			readsource.Authority{
				Knowledge: knowledgeauthority.ReadAuthority{Kind: knowledgeauthority.Review, Scope: read.Request.Event},
			},
		)
	}
	return refs
}

func ScriptMemoryReadAuthorities(call ScriptToolRecord) ([]readsource.Authority, error) {
	refs, err := KnowledgeToolSourceAuthorities(call)
	if err != nil {
		return nil, err
	}
	if call.KnowledgeRead != nil {
		refs = append(refs, readsource.Authority{Knowledge: *call.KnowledgeRead})
	}
	private, shared := false, false
	switch call.Outcome.Name {
	case "knowledge.memos", "knowledge." + agent.KnowledgeMemoRead:
		private = true
	case "knowledge.read":
		shared = true
	default:
		evidence, own, common, readErr := MemoryToolReadEvidence(call)
		if readErr != nil {
			return nil, readErr
		}
		refs = append(refs, evidence...)
		private, shared = own, common
	}
	if private {
		refs = append(
			refs,
			readsource.Authority{
				Knowledge: knowledgeauthority.ReadAuthority{
					Kind:       knowledgeauthority.PrivateMemory,
					Generation: call.MemoryReadState.PrivateGeneration,
				},
			},
		)
	}
	if shared {
		refs = append(
			refs,
			readsource.Authority{
				Knowledge: knowledgeauthority.ReadAuthority{
					Kind:       knowledgeauthority.SharedMemory,
					Generation: call.MemoryReadState.SharedGeneration,
				},
			},
		)
	}
	return MergeReadAuthorities(refs)
}

func KnowledgeToolSourceAuthorities(call ScriptToolRecord) ([]readsource.Authority, error) {
	if !strings.HasPrefix(call.Outcome.Name, "knowledge.") {
		return []readsource.Authority{}, nil
	}
	raw := bytes.TrimSpace(call.Outcome.Result)
	if len(raw) == 0 {
		return []readsource.Authority{}, nil
	}
	var items []knowledgeSourceItem
	if raw[0] == '[' {
		if err := json.Unmarshal(raw, &items); err != nil {
			return nil, err
		}
	} else {
		var value struct {
			knowledgeSourceItem

			Items []knowledgeSourceItem `json:"items"`
		}
		if err := json.Unmarshal(raw, &value); err != nil {
			return nil, err
		}
		items = append(items, value.Items...)
		items = append(items, value.knowledgeSourceItem)
	}
	groups := make([][]readsource.Authority, 0, len(items))
	for _, item := range items {
		groups = append(groups, item.ReadAuthorities)
	}
	return readsource.Merge(groups...)
}

func MemoryToolReadEvidence(call ScriptToolRecord) ([]readsource.Authority, bool, bool, error) {
	if call.Outcome.Name == "memory.sources" {
		var page conversation.Page
		if err := json.Unmarshal(call.Outcome.Result, &page); err != nil {
			return nil, false, false, err
		}
		refs, err := HistoryPageReadAuthorities(page)
		return refs, false, false, err
	}
	if !strings.HasPrefix(call.Outcome.Name, "memory.") {
		return nil, false, false, nil
	}
	var result struct {
		ReadAuthorities []readsource.Authority  `json:"read_authorities"`
		Namespace       string                  `json:"namespace"`
		Entries         []knowledge.MemoryEntry `json:"entries"`
		Summaries       []knowledge.MemoryEntry `json:"summaries"`
		Topics          []knowledge.MemoryTopic `json:"topics"`
	}
	if err := json.Unmarshal(call.Outcome.Result, &result); err != nil {
		return nil, false, false, err
	}
	refs := result.ReadAuthorities
	private, shared := result.Namespace == knowledge.MemoryPrivate, result.Namespace == knowledge.MemoryShared
	for _, entry := range append(result.Entries, result.Summaries...) {
		refs = append(refs, entry.ReadAuthorities...)
		private = private || entry.Namespace == knowledge.MemoryPrivate
		shared = shared || entry.Namespace == knowledge.MemoryShared
	}
	for _, topic := range result.Topics {
		private = private || topic.Namespace == knowledge.MemoryPrivate
		shared = shared || topic.Namespace == knowledge.MemoryShared
	}
	return refs, private, shared, nil
}

type knowledgeSourceItem struct {
	ReadAuthorities []readsource.Authority `json:"read_authorities"`
}

func PrivilegedEventAuthorities(owner string, items []core.PrivilegedReadEvent) []readsource.Authority {
	refs := []readsource.Authority{}
	for _, item := range items {
		if item.PaymentReads {
			refs = append(
				refs,
				readsource.Authority{
					Registration: passbooking.ReadAuthority{Kind: passbooking.ReadPaymentRole, Event: item.Event},
				},
			)
		}
		if item.PractitionerReads {
			refs = append(
				refs,
				readsource.Authority{Practitioner: massage.ReadAuthority{Event: item.Event, Owner: owner}},
			)
		}
	}
	return refs
}

func PrivilegedSourceRequired(name string) bool {
	switch name {
	case hostPassesPaymentsQueue,
		hostPassesPaymentsHistory,
		hostMassagePractitionerSchedule,
		hostMassagePractitionerPreferences,
		hostMassagePractitionerBookings,
		hostPrivilegesEvents,
		"food.review.queue",
		"food.review.read":
		return true
	default:
		return false
	}
}

func ScriptPrivilegedResultAuthorities(call ScriptToolRecord) ([]readsource.Authority, bool, error) {
	switch call.Outcome.Name {
	case hostPassesPaymentsQueue,
		hostPassesPaymentsHistory,
		hostMassagePractitionerSchedule,
		hostMassagePractitionerPreferences,
		hostMassagePractitionerBookings,
		hostPrivilegesEvents:
		refs, err := PrivilegedReadAuthorities(call)
		return refs, true, err
	case "food.review.queue", "food.review.read":
		if call.Food == nil {
			return nil, true, errors.New("food read provenance missing")
		}
		refs, err := readsource.Merge(
			[]readsource.Authority{{Food: legacyfood.ReadAuthority{Event: call.Food.EventID, Scope: "review"}}},
			call.ResultAuthorities,
		)
		return refs, true, err
	default:
		return nil, false, nil
	}
}

func PrivilegedReadAuthorities(call ScriptToolRecord) ([]readsource.Authority, error) {
	read := call.PrivilegedRead
	if read == nil || read.Owner == "" {
		return nil, errors.New("privileged read provenance missing")
	}
	var refs []readsource.Authority
	switch call.Outcome.Name {
	case hostPrivilegesEvents:
		if len(read.Admission) == 0 || read.Event != "" {
			return nil, errors.New("privileged event admission missing")
		}
		for _, a := range read.Admission {
			payment := a.Registration.Kind == passbooking.ReadPaymentRole
			practitioner := a.Practitioner.Owner == read.Owner && a.Practitioner.Valid()
			if !payment && !practitioner {
				return nil, errors.New("privileged event admission invalid")
			}
		}
		var page core.ReadPage[core.PrivilegedReadEvent]
		if err := json.Unmarshal(call.Outcome.Result, &page); err != nil {
			return nil, err
		}
		refs = PrivilegedEventAuthorities(read.Owner, page.Items)
		refs = append(refs, read.Admission...)
	case hostPassesPaymentsQueue, hostPassesPaymentsHistory:
		refs = []readsource.Authority{
			{Registration: passbooking.ReadAuthority{Kind: passbooking.ReadPaymentRole, Event: read.Event}},
		}
	default:
		refs = []readsource.Authority{{Practitioner: massage.ReadAuthority{Event: read.Event, Owner: read.Owner}}}
	}
	return readsource.Merge(refs, call.ResultAuthorities)
}

func PassIdentity(booking passbooking.Booking) interaction.BookingIdentity {
	return interaction.BookingIdentity{
		Event:     booking.Event,
		Owner:     booking.Owner,
		Version:   booking.Version,
		CreatedAt: booking.CreatedAt,
	}
}

func ScriptPassContext(registration *agent.RegistrationContext) []interaction.PassContextDependency {
	identities := make([]interaction.PassContextDependency, 0)
	if registration == nil {
		return identities
	}
	for _, read := range registration.Reads {
		dependency := interaction.PassContextDependency{
			Request: agent.RegistrationProposal{
				Event:  read.Request.Event,
				View:   read.Request.View,
				Target: read.Request.Target,
				Cursor: read.Request.Cursor,
			},
		}
		if read.Booking != nil && read.Booking.Version > 0 {
			identity := PassIdentity(*read.Booking)
			dependency.Booking = &identity
		} else if read.Error != "" {
			continue
		}
		if read.AdminTarget != nil && read.AdminTarget.Booking.Version > 0 {
			identity := PassIdentity(read.AdminTarget.Booking)
			dependency.TargetBooking = &identity
		}
		if read.TakeoverTarget != nil && read.TakeoverTarget.Booking.Version > 0 {
			identity := PassIdentity(read.TakeoverTarget.Booking)
			dependency.TargetBooking = &identity
		}
		dependency.QueueAuthorities = PassQueueReadAuthorities(read)
		if read.Request.View == "invitations" {
			dependency.Invitations = make([]interaction.InvitationIdentity, 0, len(read.Invitations))
		}
		for _, invitation := range read.Invitations {
			dependency.Invitations = append(
				dependency.Invitations,
				interaction.InvitationIdentity{
					Owner:     invitation.From.Owner,
					Version:   invitation.Version,
					CreatedAt: invitation.CreatedAt,
				},
			)
		}
		identities = append(identities, dependency)
	}
	return identities
}

func PassQueueView(view string) bool {
	return view == hostQueue || view == hostPaymentQueue
}

func PassQueueReadAuthorities(read agent.RegistrationReadResult) []passbooking.ReadAuthority {
	if !PassQueueView(read.Request.View) {
		return nil
	}
	result := []passbooking.ReadAuthority{}
	for _, booking := range read.Queue {
		result = append(result, passbooking.ReadAuthority{Kind: passbooking.ReadPrivileged,
			Action: agent.RegistrationAdminAssign, Event: booking.Event, Owner: booking.Owner,
			Version: booking.Version, CreatedAt: booking.CreatedAt})
	}
	for _, item := range read.PaymentQueue {
		result = append(result, passbooking.ReadAuthority{Kind: passbooking.ReadPrivileged,
			Action: hostProofAccept, Event: item.Payment.Event, Owner: item.Owner,
			Version: item.Payment.Version, CreatedAt: item.BookingCreatedAt, PaymentAttempt: item.Payment.Attempt})
	}
	return result
}

func PassQueueEvidenceComplete(read agent.RegistrationReadResult) bool {
	for _, authority := range PassQueueReadAuthorities(read) {
		if authority.Owner == "" || authority.Version <= 0 || authority.CreatedAt.IsZero() ||
			(read.Request.View == hostPaymentQueue && authority.PaymentAttempt == "") {
			return false
		}
	}
	return true
}

func PassPrivilegedReadView(name string) (string, bool) {
	switch name {
	case hostPassesAdminQueue:
		return hostQueue, true
	case hostPassesAdminTarget:
		return agent.RegistrationAdminTarget, true
	case hostPassesPaymentsReview:
		return hostPaymentQueue, true
	case hostPassesTakeoverRead:
		return agent.RegistrationTakeoverTarget, true
	default:
		return "", false
	}
}

func PassToolActions() map[string]string {
	return map[string]string{
		"passes.registration.solo":          "solo",
		"passes.registration.invite":        "invite",
		"passes.registration.accept":        "accept",
		"passes.registration.decline":       "decline",
		"passes.registration.cancel":        "cancel",
		"passes.registration.payment_admin": "payment_admin",
		hostPassesAdminAssign:               hostAdminAssign,
		"passes.admin.cancel":               hostAdminCancel,
		"passes.admin.uncouple":             "admin_uncouple",
		"passes.admin.recalculate":          "recalculate",
		"passes.payments.accept":            hostProofAccept,
		"passes.payments.reject":            "proof_reject",
		"passes.takeover.apply":             "takeover",
		"passes.takeover.received_only":     "received_only",
		hostPassesAdminQueue:                hostAdminAssign,
		hostPassesAdminTarget:               hostAdminAssign,
		hostPassesPaymentsReview:            hostProofAccept,
		hostPassesTakeoverRead:              "takeover",
		hostPassesTiers:                     hostAdminCancel,
		"passes.batch.assign":               hostAdminAssign,
		"passes.batch.cancel":               hostAdminCancel,
		"passes.batch.uncouple":             "admin_uncouple",
	}
}

func ScriptCallHasAuthorities(call ScriptToolRecord) bool {
	return PrivilegedSourceRequired(call.Outcome.Name) || call.Source != nil || call.MemoryReadState != nil ||
		call.KnowledgeRead != nil ||
		call.Outcome.Name == hostHistoryRead ||
		call.Outcome.Name == hostHistoryPage
}

func scriptDiscoveryInvalid(calls []ScriptToolRecord) (bool, error) {
	for _, call := range calls {
		if _, invalid, err := PassDiscoveryEvents(call.Outcome); invalid || err != nil {
			return invalid, err
		}
	}
	return false, nil
}

// This union lives only for one validation boundary. A later boundary always
// reauthenticates and checks current domain authority again.
func scriptRecordAuthorityUnion(owner string, record ScriptRecord) ([]readsource.Authority, error) {
	refs := record.ReadAuthorities
	for _, call := range record.Calls {
		if !ScriptCallHasAuthorities(call) {
			continue
		}
		evidence, err := ScriptCallReadAuthorities(owner, call)
		if err != nil {
			return nil, err
		}
		refs, err = readsource.Merge(refs, evidence)
		if err != nil {
			return nil, err
		}
	}
	return refs, nil
}

func scriptRecordAuthorityChecks(owner string, record ScriptRecord) ([]readsource.Authority, bool, error) {
	refs, err := scriptRecordAuthorityUnion(owner, record)
	if err == nil {
		return refs, true, nil
	}
	if errors.Is(err, readsource.ErrLimit) {
		// Preserve independently bounded carriers when their union is oversized.
		return record.ReadAuthorities, false, nil
	}
	return nil, false, err
}

func PassDiscoveryEvents(outcome agent.ScriptToolResult) ([]string, bool, error) {
	if outcome.Name != hostPassesEvents || outcome.Error != "" || len(outcome.Result) == 0 {
		return nil, false, nil
	}
	var page core.ReadPage[passbooking.NavigationEvent]
	if err := json.Unmarshal(outcome.Result, &page); err != nil {
		return nil, false, err
	}
	if page.Items == nil || len(page.Items) > core.ReadPageItems {
		return nil, true, nil
	}
	events := make([]string, 0, len(page.Items))
	for _, item := range page.Items {
		switch item.Access {
		case passbooking.NavigationPublic:
		case passbooking.NavigationOwned:
			events = append(events, item.ID)
		default:
			// Old receipts have no trusted distinction between public metadata
			// and owner membership. Invalidate output without rerunning effects.
			return nil, true, nil
		}
	}
	return events, false, nil
}
