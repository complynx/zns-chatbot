package api

import "github.com/complynx/zns-chatbot/internal/registrations"

const (
	schemaInvitations  = "invitations"
	schemaEventName    = "event_name"
	schemaNext         = "next"
	methodAccept       = "accept"
	methodDecline      = "decline"
	methodRevoke       = "revoke"
	methodInvite       = "invite"
	invitationPending  = "pending"
	schemaInvitation   = "invitation"
	schemaTargetUserID = "target_user_id"
	schemaExpiresAt    = "expires_at"
)

const schemaInvitationID = "invitation_id"

func pairInvitationMethods() []map[string]any {
	uuidField := map[string]any{manifestType: schemaString, schemaFormat: schemaUUID}
	version := map[string]any{manifestType: schemaInteger, schemaMinimum: 1}
	context := paymentMethod(
		"invitation_context",
		"Read your waitlist role, current registration version and invitation expiry for an explicit event before inviting. Explain that a pending invitation pauses your solo allocation until acceptance, decline, revoke or expiry. It reserves no place for the target. The policy read is a snapshot, not a guarantee of future eligibility.",
		map[string]any{schemaEventID: uuidField},
		[]string{schemaEventID},
		[]string{schemaEventID, schemaExpectedVersion, schemaExpiresAt, schemaRole},
	)
	context[manifestPath] = pairInvitationPath + "invitation_context"
	context[manifestOutputSchema] = invitationContextSchema()
	list := paymentMethod(
		schemaInvitations,
		"Read your incoming or outgoing invitations with event name/timezone/dates, public inviter name and account code, required opposite role, current state/version and expiry. Defaults incoming/pending; state=all includes history. UUID pagination default/max20; follow next as after. A pending state does not guarantee unexpired consent. Do not treat a historical operation receipt as current state. No profile directory, pass or payment data.",
		map[string]any{
			"direction": map[string]any{manifestType: schemaString, schemaEnum: []string{"incoming", "outgoing"}},
			schemaState: map[string]any{
				manifestType: schemaString,
				schemaEnum:   []string{invitationPending, "accepted", "declined", "revoked", "expired", "all"},
			},
			schemaEventID: uuidField,
			schemaAfter:   uuidField,
			schemaLimit: map[string]any{
				manifestType:  schemaInteger,
				schemaMinimum: 1,
				schemaMaximum: agentTaskPageLimit,
			},
		},
		nil,
		[]string{schemaInvitations},
	)
	list[manifestPath] = pairInvitationPath + schemaInvitations
	list[manifestOutputSchema] = invitationListSchema()
	invite := invitationWriteMethod(
		methodInvite,
		"Create a directed invitation only after an explicit user request and explanation of the allocation hold. Get your own current waitlist version and expiry from invitation_context. The partner may share their own account/me id as a partner code; use only a code they supplied, never enumerate Telegram IDs, usernames or profiles. A code identifies the target; it is not authentication or acceptance. The target remains unregistered/unreserved until their explicit acceptance. Do not claim delivery from creation alone.",
		map[string]any{
			schemaEventID:         uuidField,
			schemaTargetUserID:    uuidField,
			schemaExpectedVersion: version,
		},
		[]string{schemaEventID, schemaTargetUserID, schemaExpectedVersion},
	)
	methods := []map[string]any{partnerCodeMethod(), context, list, invite}
	for _, kind := range []string{methodAccept, methodDecline, methodRevoke} {
		fields := map[string]any{
			schemaEventID:         uuidField,
			schemaInvitationID:    uuidField,
			schemaExpectedVersion: version,
		}
		required := []string{schemaEventID, schemaInvitationID, schemaExpectedVersion}
		description := "Decline an incoming invitation only after the authenticated target explicitly requests it. Releases the inviter's hold, without registering the target. Read the current incoming invitation first."
		switch kind {
		case methodAccept:
			fields[schemaRole] = map[string]any{
				manifestType: schemaString,
				schemaEnum:   []string{registrations.Leader, registrations.Follower},
			}
			required = append(required, schemaRole)
			description = "Accept an incoming invitation only after the target explicitly agrees to pair with this inviter and join this event as required_role from the invitation read. This may create the target's first opposite-role waitlist registration. It does not assign passes or combine finances. Clarify ambiguous invitations and re-read on a stale version; do not silently revoke another outgoing invitation."
		case methodRevoke:
			description = "Revoke your outgoing invitation only after your request. This releases your allocation hold without changing the target's registration. Read the current outgoing invitation first."
		}
		methods = append(methods, invitationWriteMethod(kind, description, fields, required))
	}
	return methods
}

func invitationContextSchema() map[string]any {
	uuidField := map[string]any{manifestType: schemaString, schemaFormat: schemaUUID}
	return map[string]any{
		manifestType:               schemaObject,
		schemaAdditionalProperties: false,
		schemaRequired: []string{
			schemaEventID,
			schemaEventName,
			schemaTimezone,
			schemaRegistrationID,
			schemaExpectedVersion,
			schemaRole,
			schemaExpiresAt,
		},
		schemaProperties: map[string]any{
			schemaEventID:         uuidField,
			schemaEventName:       map[string]any{manifestType: schemaString},
			schemaTimezone:        map[string]any{manifestType: schemaString},
			schemaRegistrationID:  uuidField,
			schemaExpectedVersion: map[string]any{manifestType: schemaInteger, schemaMinimum: 1},
			schemaRole: map[string]any{
				manifestType: schemaString,
				schemaEnum:   []string{registrations.Leader, registrations.Follower},
			},
			schemaExpiresAt: map[string]any{manifestType: schemaString, schemaFormat: schemaDateTime},
		},
	}
}

func invitationListSchema() map[string]any {
	uuidField := map[string]any{manifestType: schemaString, schemaFormat: schemaUUID}
	instant := map[string]any{manifestType: schemaString, schemaFormat: schemaDateTime}
	view := map[string]any{
		manifestType:               schemaObject,
		schemaAdditionalProperties: false,
		schemaRequired: []string{
			schemaInvitation,
			schemaEventName,
			schemaTimezone,
			schemaStartsAt,
			schemaEndsAt,
			"inviter_user_id",
			"inviter_name",
			"required_role",
		},
		schemaProperties: map[string]any{
			schemaInvitation:  pairInvitationMethod()[manifestOutputSchema],
			schemaEventName:   map[string]any{manifestType: schemaString},
			schemaTimezone:    map[string]any{manifestType: schemaString},
			schemaStartsAt:    instant,
			schemaEndsAt:      instant,
			"inviter_user_id": uuidField,
			"inviter_name":    map[string]any{manifestType: schemaString},
			"required_role": map[string]any{
				manifestType: schemaString,
				schemaEnum:   []string{registrations.Leader, registrations.Follower},
			},
		},
	}
	return map[string]any{
		manifestType:               schemaObject,
		schemaAdditionalProperties: false,
		schemaRequired:             []string{schemaInvitations},
		schemaProperties: map[string]any{
			schemaInvitations: map[string]any{manifestType: schemaArray, schemaItems: view},
			schemaNext:        uuidField,
		},
	}
}

func invitationWriteMethod(kind, description string, fields map[string]any, required []string) map[string]any {
	method := paymentMethod(
		kind,
		description+" After uncertainty reconcile the same operation ID. Missing status is unknown; never create a replacement operation blindly. The receipt is historical; use invitations for current state.",
		fields,
		required,
		nil,
	)
	method[manifestPath] = pairInvitationPath + kind
	method[manifestEffect] = effectWrite
	method[manifestOutputSchema] = map[string]any{
		manifestType:   schemaObject,
		schemaRequired: []string{schemaInvitation},
		schemaProperties: map[string]any{
			schemaInvitation: pairInvitationMethod()[manifestOutputSchema], schemaPair: pairSchema()},
	}
	method[manifestIdempotency] = true
	method[manifestStatusPath] = pairInvitationPath + kind + "/status"
	method[manifestDedupWindow] = agentRegistrationDedupSeconds
	return method
}
