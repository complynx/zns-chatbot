package bot

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/complynx/zns-chatbot/platform/internal/core"
)

func TestPassMenuStaleSourceClassification(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name  string
		err   error
		stale bool
	}{
		{"canonical delivery source", &core.ProblemError{Status: 409, Code: "pass_source_stale"}, true},
		{"wrapped delivery source", fmt.Errorf("enqueue: %w", &core.ProblemError{Status: 409, Code: "pass_source_stale"}), true},
		{"derived source", &core.ProblemError{Status: 409, Code: "source_stale"}, true},
		{"history source", &core.ProblemError{Status: 409, Code: "history_stale"}, true},
		{"unrelated conflict", &core.ProblemError{Status: 409, Code: "version_conflict"}, false},
		{"temporary failure", errors.New("source unavailable"), false},
		{"success", nil, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, test.stale, stalePassMenuSource(test.err))
		})
	}
}
