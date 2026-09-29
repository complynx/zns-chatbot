package migrate

import "encoding/json"

// Runtime provenance stores only pass-domain fields. The complete immutable
// source stays in the private archive and is bound by its original digest.
func passRuntimeRecord(row PassPlanRecord) json.RawMessage {
	if row.Source != usersSource {
		return row.Record
	}
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(row.Record, &fields)
	projection := map[string]any{"parent_sha256": row.Legacy.RecordSHA256, "field": row.Field, "source": row.Legacy}
	if row.Field != "" {
		projection["record"] = fields[row.Field]
	} else {
		for _, key := range []string{"proof_admins", "notified_passport_data_required"} {
			if value, exists := fields[key]; exists {
				projection[key] = value
			}
		}
		_, exists := fields["passport_number"]
		projection["passport_field_present"] = exists
	}
	raw, _ := json.Marshal(projection)
	return raw
}
