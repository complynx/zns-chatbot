package migrate

import (
	"bytes"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"
)

const foodSourceFields = "_id bot_id user_id pass_key order_details total is_complete activities created_at last_updated proof_admin payment_status proof_file proof_received_date payment_confirmed_by payment_confirmed_date payment_rejected_by payment_rejected_date activities_payment_status activities_proof_file activities_proof_received_date activities_payment_confirmed_by activities_payment_confirmed_date activities_payment_rejected_by activities_payment_rejected_date notification_first_sent notification_last_sent origin_info"

// FoodCandidate keeps the two source payment histories independent. Generation
// zero is a target compatibility identity; the source has no attempt token.
type FoodCandidate struct {
	ID              string          `json:"id"`
	Event           string          `json:"event"`
	Owner           int64           `json:"owner"`
	Meals           json.RawMessage `json:"meals"`
	Total           int64           `json:"total"`
	Complete        bool            `json:"complete"`
	Activities      map[string]bool `json:"activities"`
	Created         time.Time       `json:"created"`
	Updated         *time.Time      `json:"updated,omitempty"`
	Admin           int64           `json:"admin"`
	MealPayment     FoodPayment     `json:"meal_payment"`
	ActivityPayment FoodPayment     `json:"activity_payment"`
	FirstSent       bool            `json:"first_sent"`
	LastSent        bool            `json:"last_sent"`
}

type FoodPayment struct {
	Status      string     `json:"status"`
	Proof       string     `json:"proof"`
	Received    *time.Time `json:"received,omitempty"`
	ConfirmedBy int64      `json:"confirmed_by"`
	ConfirmedAt *time.Time `json:"confirmed_at,omitempty"`
	RejectedBy  int64      `json:"rejected_by"`
	RejectedAt  *time.Time `json:"rejected_at,omitempty"`
}

func convertFood(raw []byte, bot int64) (*FoodCandidate, error) {
	f, err := objectFields(raw, foodSourceFields)
	if err != nil {
		return nil, errors.New("food_field_unmapped")
	}
	if err = validateFoodOrigin(f["origin_info"]); err != nil {
		return nil, err
	}
	p := &FoodCandidate{Meals: json.RawMessage(`{}`), Activities: map[string]bool{}}
	id, err := orderTargetID(f["_id"], bot)
	if err != nil {
		return nil, err
	}
	p.ID = strings.Replace(id, "legacy-order:", "legacy-food:", 1)
	var valid bool
	p.Owner, valid = telegramNumber(f["user_id"])
	if !valid || p.Owner <= 0 {
		return nil, errors.New("food_owner_invalid")
	}
	p.Event, err = orderString(f, "pass_key", true)
	if err != nil || !tokenPattern.MatchString(p.Event) {
		return nil, errors.New("food_event_invalid")
	}
	p.Created, err = eventInstant(f["created_at"])
	if err != nil {
		return nil, errors.New("food_created_time_unresolved")
	}
	if err = foodOptionalTime(f["last_updated"], &p.Updated); err != nil {
		return nil, err
	}
	if err = p.readFoodChoices(f); err != nil {
		return nil, err
	}
	if err = p.readFoodActivities(f); err != nil {
		return nil, err
	}
	if p.Admin, err = foodOptionalActor(f["proof_admin"]); err != nil {
		return nil, err
	}
	if p.MealPayment, err = foodPayment(f, ""); err != nil {
		return nil, err
	}
	if p.ActivityPayment, err = foodPayment(f, "activities_"); err != nil {
		return nil, err
	}
	if err = foodOptionalBool(f, "notification_first_sent", &p.FirstSent); err != nil {
		return nil, err
	}
	if err = foodOptionalBool(f, "notification_last_sent", &p.LastSent); err != nil {
		return nil, err
	}
	return p, nil
}

func foodOptionalBool(fields map[string]json.RawMessage, key string, target *bool) error {
	raw, exists := fields[key]
	if !exists {
		return nil
	}
	if !bytes.Equal(raw, []byte("true")) && !bytes.Equal(raw, []byte("false")) {
		return errors.New("food_boolean_invalid")
	}
	return json.Unmarshal(raw, target)
}

func foodOptionalTime(raw json.RawMessage, target **time.Time) error {
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil
	}
	value, err := eventInstant(raw)
	if err != nil {
		return errors.New("food_datetime_unresolved")
	}
	*target = &value
	return nil
}

func foodOptionalActor(raw json.RawMessage) (int64, error) {
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return 0, nil
	}
	actor, valid := telegramNumber(raw)
	if !valid || actor <= 0 {
		return 0, errors.New("food_actor_invalid")
	}
	return actor, nil
}

func foodPayment(fields map[string]json.RawMessage, prefix string) (FoodPayment, error) {
	p := FoodPayment{Status: "pending"}
	if raw := fields[prefix+"payment_status"]; len(raw) > 0 && !bytes.Equal(raw, []byte("null")) {
		if json.Unmarshal(raw, &p.Status) != nil {
			return p, errors.New("food_payment_status_invalid")
		}
		if p.Status != "paid" && p.Status != "rejected" && p.Status != "proof_submitted" {
			return p, errors.New("food_payment_status_unmapped")
		}
	}
	var err error
	if raw := fields[prefix+registrationProofField]; len(raw) > 0 && !bytes.Equal(raw, []byte("null")) {
		if json.Unmarshal(raw, &p.Proof) != nil || p.Proof == "" {
			return p, errors.New("food_proof_invalid")
		}
	}
	if err = foodOptionalTime(fields[prefix+"proof_received_date"], &p.Received); err != nil {
		return p, err
	}
	if err = foodOptionalTime(fields[prefix+"payment_confirmed_date"], &p.ConfirmedAt); err != nil {
		return p, err
	}
	if err = foodOptionalTime(fields[prefix+"payment_rejected_date"], &p.RejectedAt); err != nil {
		return p, err
	}
	if p.ConfirmedBy, err = foodOptionalActor(fields[prefix+"payment_confirmed_by"]); err != nil {
		return p, err
	}
	if p.RejectedBy, err = foodOptionalActor(fields[prefix+"payment_rejected_by"]); err != nil {
		return p, err
	}
	return p, nil
}

func (p *FoodCandidate) readFoodChoices(f map[string]json.RawMessage) error {
	if value, ok := f["order_details"]; ok && !bytes.Equal(value, []byte("null")) {
		var days map[string]json.RawMessage
		if json.Unmarshal(value, &days) != nil || days == nil {
			return errors.New("food_meals_invalid")
		}
		p.Meals = bytes.Clone(value)
	}
	if value, ok := f["total"]; ok {
		var err error
		p.Total, err = orderMoney(value)
		if err != nil {
			return errors.New("food_total_invalid")
		}
	}
	if err := foodOptionalBool(f, "is_complete", &p.Complete); err != nil {
		return err
	}
	return nil
}

func (p *FoodCandidate) readFoodActivities(f map[string]json.RawMessage) error {
	if value, ok := f["activities"]; ok {
		var flags map[string]json.RawMessage
		if json.Unmarshal(value, &flags) != nil || flags == nil {
			return errors.New("food_activities_invalid")
		}
		for key := range flags {
			if key != "open" && key != foodYoga && key != foodCacao && key != foodSoundHealing {
				return errors.New("food_activity_unmapped")
			}
			var selected bool
			if err := foodOptionalBool(flags, key, &selected); err != nil {
				return err
			}
			p.Activities[key] = selected
		}
	}
	return nil
}

func validateFoodOrigin(raw json.RawMessage) error {
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil
	}
	fields, err := objectFields(raw, "message_id chat_id")
	if err != nil {
		return errors.New("food_origin_invalid")
	}
	for _, key := range []string{"message_id", "chat_id"} {
		id, parseErr := recordID(fields[key])
		if parseErr != nil || !strings.HasPrefix(id, "integer:") {
			return errors.New("food_origin_invalid")
		}
		value, parseErr := strconv.ParseInt(strings.TrimPrefix(id, "integer:"), 10, 64)
		if parseErr != nil || value == 0 || value <= -maxTelegramID || value >= maxTelegramID ||
			key == "message_id" && value < 0 {
			return errors.New("food_origin_invalid")
		}
	}
	return nil
}
