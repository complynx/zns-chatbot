package migrate

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"slices"
)

func userPassRegistry(root *os.Root, manifest Manifest, limits Limits) (map[string]bool, error) {
	registry := map[string]bool{}
	collection := ""
	for _, source := range manifest.Coverage {
		if source.Domain == eventsSource && source.Status == sourceIncluded {
			collection = source.Name
		}
	}
	if collection == "" {
		return registry, nil
	}
	rows := OrderPlan{BotID: manifest.BotID}
	budget := &planBudgetWriter{target: io.Discard, remaining: maxUserPlanBytes}
	for _, entry := range manifest.Files {
		if entry.Source == eventsSource {
			if entry.Kind != recordsFileKind {
				return nil, errors.New("events_source_requires_records")
			}
			if err := readOrderPlanFile(root, entry, collection, limits, &rows, budget); err != nil {
				return nil, err
			}
		}
	}
	for _, row := range rows.Records {
		var f map[string]json.RawMessage
		_ = json.Unmarshal(row.Record, &f)
		var key string
		if json.Unmarshal(f["key"], &key) != nil || !tokenPattern.MatchString(key) {
			return nil, errors.New("event_key_invalid")
		}
		registry[key] = true
	}
	return registry, nil
}
func deferUserPassFields(record map[string]json.RawMessage, planned *UserPlanRecord, registry map[string]bool) {
	if planned.Candidate == nil || planned.Status != userCandidate {
		return
	}
	deferred := false
	unmapped := false
	for index, field := range planned.Fields {
		if field.Disposition != userFieldUnmapped {
			continue
		}
		valid := deferredPassField(record, field.Field, registry)
		if valid {
			planned.Fields[index].Disposition = "deferred_passes"
			deferred = true
		} else {
			unmapped = true
		}
	}
	if !unmapped {
		planned.Blockers = slices.DeleteFunc(
			planned.Blockers,
			func(value string) bool { return value == "unmapped_fields" },
		)
	}
	if deferred {
		projection := map[string]json.RawMessage{}
		for _, field := range planned.Fields {
			if field.Disposition == "deferred_passes" {
				projection[field.Field] = record[field.Field]
			}
		}
		planned.DeferredPasses, _ = json.Marshal(projection)
	}
}

func deferredPassField(record map[string]json.RawMessage, field string, registry map[string]bool) bool {
	switch {
	case registry[field]:
		embedded, err := objectFields(record[field], registrationFields)
		var state string
		return err == nil && json.Unmarshal(embedded["state"], &state) == nil && registrationState(state)
	case field == "proof_admins":
		var prefs map[string]json.RawMessage
		if json.Unmarshal(record[field], &prefs) != nil || prefs == nil {
			return false
		}
		for event, value := range prefs {
			_, ok := telegramNumber(value)
			if !registry[event] || !ok {
				return false
			}
		}
		return true
	case field == "notified_passport_data_required":
		return true
	default:
		return false
	}
}
