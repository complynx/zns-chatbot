package appclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"

	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

const knowledgeAuthorityField = "read_authorities"
const knowledgeResponseFraming = 4096
const knowledgeResponseBytes = MaxAPIBytes + readsource.MaxAuthorityBytes + knowledgeResponseFraming

// Knowledge responses carry one host-only authority union. Its budget does not
// reduce the existing body allowance or enlarge unrelated application replies.
type knowledgeEnvelope struct{ output any }

func (e knowledgeEnvelope) UnmarshalJSON(data []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	if fields == nil {
		return errors.New("invalid knowledge response")
	}
	if metadata, present := fields[knowledgeAuthorityField]; present {
		if len(metadata) > readsource.MaxAuthorityBytes {
			return &apiResponseLimitError{}
		}
		var refs []readsource.Authority
		decoder := json.NewDecoder(bytes.NewReader(metadata))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&refs); err != nil {
			return err
		}
		if refs == nil || !readsource.Valid(refs) {
			return errors.New("invalid knowledge source authority")
		}
		delete(fields, knowledgeAuthorityField)
	}
	body, err := json.Marshal(fields)
	if err != nil {
		return err
	}
	if len(body) > MaxAPIBytes {
		return &apiResponseLimitError{}
	}
	return json.Unmarshal(data, e.output)
}

func (c Client) knowledgeCall(ctx context.Context, owner, method, path string, input, output any) error {
	var body []byte
	var err error
	if input != nil {
		body, err = json.Marshal(input)
		if err != nil {
			return err
		}
	}
	token, err := c.UserToken(ctx, owner)
	if err != nil {
		return err
	}
	return requestAuthorizedLimit(
		ctx,
		c.Base,
		c.HTTP,
		token,
		"",
		method,
		path,
		body,
		&knowledgeEnvelope{output},
		knowledgeResponseBytes,
	)
}
