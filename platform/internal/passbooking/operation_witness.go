package passbooking

import (
	"encoding/json"
	"slices"
	"strings"
)

type OperationFamily string

const (
	OperationCommand    OperationFamily = "command"
	OperationAssignment OperationFamily = "assignment"
	OperationBatch      OperationFamily = "batch"
)

// OperationWitness identifies an exact admitted request without retaining its
// private options. It is host-only metadata, never permission or executable input.
type OperationWitness struct {
	Family           OperationFamily `json:"family"`
	Owner            string          `json:"owner"`
	Event            string          `json:"event"`
	Key              string          `json:"key"`
	Action           string          `json:"action"`
	Target           string          `json:"target,omitempty"`
	Version          int64           `json:"version,omitempty"`
	TargetVersion    int64           `json:"target_version,omitempty"`
	InviteTelegramID int64           `json:"invite_telegram_id,omitempty"`
	PaymentAdmin     string          `json:"payment_admin,omitempty"`
	QueueInvitation  bool            `json:"queue_invitation,omitempty"`
	Recipients       []int64         `json:"recipients,omitempty"`
	Digest           string          `json:"digest"`
}

// CommandOperationWitness runs only after the host binds the final key.
// It uses the same encoding and hash as the domain's canonical receipt writers.
func CommandOperationWitness(actor string, c Command) (OperationWitness, error) {
	if err := validate(c); err != nil {
		return OperationWitness{}, err
	}
	w := OperationWitness{Family: OperationCommand, Owner: actor, Event: c.Event, Key: c.Key, Action: c.Name,
		Target: c.Target, Version: c.Version, TargetVersion: c.TargetVersion, InviteTelegramID: c.InviteTelegramID,
		PaymentAdmin: c.PaymentAdmin, QueueInvitation: c.QueueInvitation}
	return operationWitness(w, c)
}

func AssignmentOperationWitness(actor string, c AdminAssignment) (OperationWitness, error) {
	if err := validateAdminAssignment(c); err != nil {
		return OperationWitness{}, err
	}
	w := OperationWitness{
		Family:        OperationAssignment,
		Owner:         actor,
		Event:         c.Event,
		Key:           c.Key,
		Action:        commandAdminAssign,
		Target:        c.Target,
		Version:       c.Version,
		TargetVersion: c.TargetVersion,
	}
	return operationWitness(w, c)
}

func BatchOperationWitness(actor string, c RuntimeBatch) (OperationWitness, error) {
	if err := validateRuntimeBatch(c); err != nil {
		return OperationWitness{}, err
	}
	w := OperationWitness{Family: OperationBatch, Owner: actor, Event: c.Event, Key: c.Key, Action: c.Action,
		Recipients: slices.Clone(c.Recipients)}
	return operationWitness(w, c)
}

func operationWitness(w OperationWitness, request any) (OperationWitness, error) {
	raw, err := json.Marshal(request)
	if err != nil {
		return OperationWitness{}, err
	}
	w.Digest = hash(raw)
	if !w.Valid(w.Owner) {
		return OperationWitness{}, invalid()
	}
	return w, nil
}

func (w OperationWitness) Valid(actor string) bool {
	const identityBytes = 200
	const digestBytes = 64
	if w.Owner != actor || !boundedText(w.Owner, identityBytes, false) || !boundedText(w.Event, identityBytes, false) ||
		!boundedText(w.Key, identityBytes, false) || !boundedText(w.Target, identityBytes, true) ||
		!boundedText(w.PaymentAdmin, identityBytes, true) || w.Version < 0 || w.TargetVersion < 0 ||
		w.InviteTelegramID < 0 || len(w.Digest) != digestBytes || strings.Trim(w.Digest, "0123456789abcdef") != "" {
		return false
	}
	switch w.Family {
	case OperationCommand:
		return len(w.Recipients) == 0 && validate(Command{Name: w.Action, Event: w.Event, Key: w.Key,
			Target: w.Target, Version: w.Version, TargetVersion: w.TargetVersion, InviteTelegramID: w.InviteTelegramID,
			PaymentAdmin: w.PaymentAdmin, QueueInvitation: w.QueueInvitation}) == nil
	case OperationAssignment:
		return w.Action == commandAdminAssign && w.Target != "" && len(w.Recipients) == 0 &&
			w.InviteTelegramID == 0 && w.PaymentAdmin == "" && !w.QueueInvitation
	case OperationBatch:
		return w.Target == "" && w.Version == 0 && w.TargetVersion == 0 && w.InviteTelegramID == 0 &&
			w.PaymentAdmin == "" && !w.QueueInvitation && validateRuntimeBatch(RuntimeBatch{Event: w.Event, Key: w.Key,
			Action: w.Action, Recipients: w.Recipients}) == nil
	default:
		return false
	}
}
