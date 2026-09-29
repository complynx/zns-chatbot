package scriptworker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
)

// Serve consumes one bounded JSON request through EOF, writes one response, and
// returns. A supervising process must bound blocking reads/writes and heap use.
func Serve(ctx context.Context, input io.Reader, output io.Writer) error {
	request, err := readRequest(input)
	var response Response
	if err != nil {
		response.Error = invalidRequestCode
	} else {
		response.Result, err = Evaluate(ctx, request)
		if err != nil {
			response.Error = errorCode(err)
		}
	}
	encoder := json.NewEncoder(output)
	// The result already contains validated JSON; HTML escaping would expand it
	// after its byte budget was checked.
	encoder.SetEscapeHTML(false)
	return encoder.Encode(response)
}

func readRequest(input io.Reader) (Request, error) {
	data, err := io.ReadAll(io.LimitReader(input, MaxRequestBytes+1))
	if err != nil || len(data) > MaxRequestBytes {
		return Request{}, ErrRequest
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var request Request
	if err = decoder.Decode(&request); err != nil {
		return Request{}, ErrRequest
	}
	if !errors.Is(decoder.Decode(new(any)), io.EOF) {
		return Request{}, ErrRequest
	}
	return request, nil
}

func errorCode(err error) string {
	switch {
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, ErrRequest):
		return invalidRequestCode
	case errors.Is(err, ErrResult):
		return "invalid_result"
	default:
		return "execution_failed"
	}
}

const invalidRequestCode = "invalid_request"
