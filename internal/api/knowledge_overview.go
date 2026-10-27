package api

import "github.com/google/uuid"

type knowledgeOverviewArguments struct {
	After uuid.UUID `json:"after,omitempty"`
}
