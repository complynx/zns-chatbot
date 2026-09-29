package migrate

import (
	"bytes"
	"encoding/json"
	"errors"
	"strconv"
	"time"
)

const passesSource = "passes"
const passPlanVersion = 4

const registrationFields = "_id bot_id user_id pass_key state role type couple price pass_type_index assignment_tier_number date_created date_assignment proof_admin proof_admin_received proof_admin_accepted proof_received proof_accepted proof_rejected proof_file skip_in_balance_count comment notified_deadline_close notified_deadline_close2 notified_food_first notified_food_last sent_to_hype_thread"

type PassCandidate struct {
	AnnouncementMarker json.RawMessage `json:"announcement_marker,omitempty"`
	AssignmentTier     *int64          `json:"assignment_tier_number,omitempty"`
	ActiveReceipt      bool            `json:"active_receipt"`
	Event              string          `json:"event"`
	TelegramID         int64           `json:"telegram_id"`
	State              string          `json:"state"`
	Role               string          `json:"role"`
	Kind               string          `json:"kind"`
	Partner            int64           `json:"partner"`
	Admin              int64           `json:"admin"`
	Created            time.Time       `json:"created"`
	Assigned           *time.Time      `json:"assigned,omitempty"`
	Price              *int64          `json:"price,omitempty"`
	Tier               *int64          `json:"tier,omitempty"`
	SkipBalance        *bool           `json:"skip_balance,omitempty"`
	Comment            string          `json:"comment"`
	FirstReminder      *time.Time      `json:"first_reminder,omitempty"`
	SecondReminder     *time.Time      `json:"second_reminder,omitempty"`
	ProofFile          string          `json:"proof_file"`
	Received           *time.Time      `json:"received,omitempty"`
	Accepted           *time.Time      `json:"accepted,omitempty"`
	Rejected           *time.Time      `json:"rejected,omitempty"`
	ReceivingAdmin     int64           `json:"receiving_admin"`
	ReviewingAdmin     int64           `json:"reviewing_admin"`
}

const (
	registrationPending       = "waiting-for-couple"
	registrationAssigned      = "assigned"
	registrationFree          = "free_pass"
	registrationProofField    = "proof_file"
	registrationReceivedField = "proof_received"
	registrationKindLimit     = 80
	registrationCommentLimit  = 2000
	registrationIntegerLimit  = 2147483647
)

func convertPass(raw []byte) (*PassCandidate, error) {
	f, err := objectFields(raw, registrationFields)
	if err != nil {
		return nil, errors.New("pass_field_unmapped")
	}
	// Food consumes these flags through its separate deferred domain; passes only validates and retains them.
	for _, field := range []string{"notified_food_first", "notified_food_last"} {
		if value, exists := f[field]; exists {
			var flag bool
			if bytes.Equal(value, []byte("null")) || json.Unmarshal(value, &flag) != nil {
				return nil, errors.New("pass_food_marker_invalid")
			}
		}
	}
	p := &PassCandidate{Kind: "solo", AnnouncementMarker: f["sent_to_hype_thread"]}
	for _, read := range []func(map[string]json.RawMessage) error{p.readBasics, p.readActors, p.readTimes, p.readCounts} {
		if err = read(f); err != nil {
			return nil, err
		}
	}
	return p, p.validateState()
}
func (p *PassCandidate) readBasics(f map[string]json.RawMessage) error {
	var err error
	if p.TelegramID, _ = telegramNumber(f["user_id"]); p.TelegramID == 0 {
		return errors.New("pass_owner_invalid")
	}
	if p.Event, err = orderString(f, "pass_key", true); err != nil || !tokenPattern.MatchString(p.Event) {
		return errors.New("pass_event_invalid")
	}
	if p.State, err = orderString(f, "state", true); err != nil {
		return err
	}
	if !registrationState(p.State) {
		return errors.New("pass_state_unmapped")
	}
	if p.Role, err = orderString(f, "role", true); err != nil || p.Role != "leader" && p.Role != "follower" {
		return errors.New("pass_role_invalid")
	}
	if _, ok := f["type"]; ok {
		if p.Kind, err = orderString(f, "type", true); err != nil || len([]rune(p.Kind)) > registrationKindLimit {
			return errors.New("pass_kind_invalid")
		}
	}
	if p.Comment, err = orderString(
		f,
		"comment",
		false,
	); err != nil ||
		len([]rune(p.Comment)) > registrationCommentLimit {
		return errors.New("pass_comment_invalid")
	}
	if p.Created, err = eventInstant(f["date_created"]); err != nil {
		return errors.New("pass_datetime_unresolved")
	}
	p.ProofFile, err = orderProofFile(f)
	return err
}
func registrationState(state string) bool {
	return state == "waitlist" || state == registrationPending || state == registrationAssigned ||
		state == orderStatePaid
}
func (p *PassCandidate) readActors(f map[string]json.RawMessage) error {
	for key, target := range map[string]*int64{"couple": &p.Partner, "proof_admin": &p.Admin, "proof_admin_received": &p.ReceivingAdmin, "proof_admin_accepted": &p.ReviewingAdmin} {
		value, ok := f[key]
		if !ok || bytes.Equal(value, []byte("null")) {
			continue
		}
		id, valid := telegramNumber(value)
		if !valid {
			return errors.New("pass_actor_invalid")
		}
		*target = id
	}
	if p.Admin == 0 {
		return errors.New("pass_current_admin_unresolved")
	}
	if p.Partner == p.TelegramID || p.State == registrationPending && p.Partner == 0 {
		return errors.New("pass_pair_invalid")
	}
	return nil
}
func (p *PassCandidate) readTimes(f map[string]json.RawMessage) error {
	for key, target := range map[string]**time.Time{"date_assignment": &p.Assigned, registrationReceivedField: &p.Received, "proof_accepted": &p.Accepted, "proof_rejected": &p.Rejected, "notified_deadline_close": &p.FirstReminder, "notified_deadline_close2": &p.SecondReminder} {
		value, ok := f[key]
		if !ok || bytes.Equal(value, []byte("null")) {
			continue
		}
		at, err := eventInstant(value)
		if err != nil {
			return errors.New("pass_datetime_unresolved")
		}
		*target = &at
	}
	return nil
}
func (p *PassCandidate) readCounts(f map[string]json.RawMessage) error {
	for key, target := range map[string]**int64{"price": &p.Price, "pass_type_index": &p.Tier, "assignment_tier_number": &p.AssignmentTier} {
		value, ok := f[key]
		if !ok || bytes.Equal(value, []byte("null")) {
			continue
		}
		n, err := orderSignedNumber(value)
		if err != nil || n < 0 || n > registrationIntegerLimit || key == "assignment_tier_number" && n == 0 {
			return errors.New("pass_integer_unresolved")
		}
		*target = &n
	}
	if value, ok := f["skip_in_balance_count"]; ok {
		var flag bool
		if bytes.Equal(value, []byte("null")) {
			return nil
		}
		if json.Unmarshal(value, &flag) != nil {
			return errors.New("pass_balance_policy_invalid")
		}
		p.SkipBalance = &flag
	}
	return nil
}
func (p *PassCandidate) validateState() error {
	if p.State == registrationAssigned || p.State == orderStatePaid {
		if p.Assigned == nil || p.Price == nil {
			return errors.New("pass_assignment_unresolved")
		}
	} else {
		p.Assigned = nil
		p.Price = nil
		p.Tier = nil
		p.SkipBalance = nil
	}
	if p.ProofFile == registrationFree && (p.State != orderStatePaid || p.Price == nil || *p.Price != 0) {
		return errors.New("pass_free_state_unresolved")
	}
	if p.ProofFile == "" || p.ProofFile == registrationFree {
		return nil
	}
	if p.Received == nil {
		return errors.New("pass_proof_received_unresolved")
	}
	p.ActiveReceipt = p.Assigned != nil && !p.Received.Before(*p.Assigned)
	if !p.ActiveReceipt {
		return nil
	}
	if p.State == registrationAssigned && (p.Rejected == nil || p.Rejected.Before(*p.Received)) {
		return errors.New("pass_proof_decision_unresolved")
	}
	if p.State == orderStatePaid && p.Rejected != nil && !p.Rejected.Before(*p.Received) &&
		(p.Accepted == nil || p.Rejected.After(*p.Accepted)) {
		return errors.New("pass_proof_decision_unresolved")
	}
	return nil
}
func passIdentity(event string, id int64) string { return event + ":" + strconv.FormatInt(id, 10) }
