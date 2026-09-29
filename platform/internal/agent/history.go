package agent

import (
	"bytes"
	"encoding/json"
	"errors"

	"github.com/complynx/zns-chatbot/platform/internal/conversation"
)

const MaxHistoryReads = 2
const planHistoryField = "history_action"

type HistoryProposal struct {
	Before int64 `json:"before"`
}
type HistoryContext struct {
	Summary   conversation.Summary `json:"summary"`
	Gap       bool                 `json:"gap"`
	BeforeID  int64                `json:"before_id"`
	Remaining int                  `json:"remaining"`
	Reads     []conversation.Page  `json:"reads"`
	Omitted   bool                 `json:"omitted"`
}

func validateHistoryPlan(p Plan) error {
	if p.HistoryAction == nil {
		return nil
	}
	if p.HistoryAction.Before < 0 || p.Action != nil || p.OrderAction != nil || p.ProfileAction != nil ||
		p.MediaAction != nil ||
		p.KnowledgeAction != nil ||
		p.ScriptAction != nil {
		return errors.New("invalid or conflicting history request")
	}
	return nil
}

func codexHistoryFields(data []byte) error {
	fields, err := codexObject(data)
	if err != nil || len(fields) != 1 || bytes.Equal(fields["before"], []byte("null")) {
		return errors.New("invalid history request")
	}
	var before int64
	if json.Unmarshal(fields["before"], &before) != nil || before < 0 {
		return errors.New("invalid history cursor")
	}
	return nil
}
