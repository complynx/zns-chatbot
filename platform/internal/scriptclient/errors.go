package scriptclient

import (
	"context"
	"encoding/json"
	"errors"
)

// ErrInvalidResult identifies a malformed worker response without its contents.
var ErrInvalidResult = errors.New("invalid script response")

// ErrResultLimit identifies a response exceeding the client's byte budget.
var ErrResultLimit = errors.New("script response exceeds limit")

// ErrInvalidRequest identifies a worker rejection of the request shape.
var ErrInvalidRequest = errors.New("invalid script request")

// ErrExecution identifies a worker-reported failure without exception contents.
var ErrExecution = errors.New("script execution failed")

// Only finite protocol codes cross the boundary; arbitrary worker text is discarded.
func workerError(value json.RawMessage) error {
	var code string
	if json.Unmarshal(value, &code) != nil {
		return ErrInvalidResult
	}
	switch code {
	case "timeout":
		return context.DeadlineExceeded
	case "canceled":
		return context.Canceled
	case "invalid_result":
		return ErrInvalidResult
	case "invalid_request":
		return ErrInvalidRequest
	case "execution_failed":
		return ErrExecution
	default:
		return errors.New("script execution failed")
	}
}
