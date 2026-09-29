package conversation

import (
	"github.com/complynx/zns-chatbot/platform/internal/conversation/dbgen"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

func eventFromRow(row dbgen.ReadEventsRow, authorities []readsource.Authority) Event {
	return Event{ID: row.ID, Kind: row.Kind, Text: row.Text, Details: row.Details, Omitted: row.Omitted,
		At: row.At, HasFullText: row.HasFullText, OmissionReason: row.OmissionReason, ReadAuthorities: authorities}
}
