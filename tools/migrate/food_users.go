package migrate

import "encoding/json"

// The food marker projection is a separate completion obligation even when the
// enclosing pass registration has already been imported.
func deferUserFoodFields(record map[string]json.RawMessage, planned *UserPlanRecord, registry map[string]bool) {
	if planned.Candidate == nil || planned.Status != userCandidate {
		return
	}
	projection := map[string]json.RawMessage{}
	for event := range registry {
		var fields map[string]json.RawMessage
		if json.Unmarshal(record[event], &fields) != nil {
			continue
		}
		_, first := fields[foodFirstMarker]
		_, last := fields[foodLastMarker]
		if first || last {
			projection[event] = record[event]
		}
	}
	if len(projection) > 0 {
		planned.DeferredFood, _ = json.Marshal(projection)
	}
}
