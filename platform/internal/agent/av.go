package agent

import "github.com/complynx/zns-chatbot/platform/internal/mediaproc"

// AVInspectionContext reports host-executed inspections for this user input.
// Completed contains only this planning attempt; remaining includes earlier retries.
type AVInspectionContext struct {
	Remaining int            `json:"remaining"`
	Completed []AVInspection `json:"completed"`
}

type AVInspection struct {
	MediaID    string `json:"media_id"`
	StartMS    int64  `json:"start_ms"`
	EndMS      int64  `json:"end_ms"`
	FrameCount int    `json:"frame_count"`
}

// AVContext is private current-input evidence, never interaction history.
// Sparse frames and failed/no-text transcription must not imply complete coverage.
type AVContext struct {
	ID         string               `json:"id"`
	Kind       string               `json:"kind"`
	Status     string               `json:"status"`
	Duration   mediaproc.Rational   `json:"duration"`
	Transcript mediaproc.Transcript `json:"transcript"`
	Sampling   *mediaproc.Sampling  `json:"sampling,omitempty"`
}
