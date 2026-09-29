package conversation

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/core"
)

func TestHistoryResultBudgetAndFailureReturnZero(t *testing.T) {
	t.Parallel()
	page := Page{Events: []Event{{ID: 1, Details: json.RawMessage(`{"body":"` + strings.Repeat("<", 200000) + `"}`)}}}
	result, err := boundedHistoryResult(page, nil)
	require.Empty(t, result)
	var problem *core.ProblemError
	require.ErrorAs(t, err, &problem)
	require.Equal(t, "read_result_limit", problem.Code)
	failure := errors.New("commit failed")
	result, err = boundedHistoryResult(Page{Generation: 4}, failure)
	require.Empty(t, result)
	require.ErrorIs(t, err, failure)
	valid := Page{Generation: 4, Events: []Event{{ID: 2, Details: json.RawMessage(`{}`), Text: "Привет"}}}
	result, err = boundedHistoryResult(valid, nil)
	require.NoError(t, err)
	require.Equal(t, valid, result)
}
