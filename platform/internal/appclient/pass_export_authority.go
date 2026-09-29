package appclient

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

func (c Host) ExportPassSnapshot(ctx context.Context, owner string) (passbooking.ExportSnapshot, error) {
	if c.LocalDerived != nil {
		return c.localExportPassSnapshot(ctx, owner)
	}
	var result passbooking.ExportSnapshot
	if c.UserToken == nil {
		return result, identity.ErrZitadelIdentity
	}
	token, err := c.UserToken(ctx, owner)
	if err != nil {
		return result, err
	}
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		c.Base+"/internal/passes/export-snapshot",
		http.NoBody,
	)
	if err != nil {
		return result, clientBoundaryError(ctx, err, "invalid export request")
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("X-Zns-Derivation", c.Signer.DerivedMutationToken(owner))
	client := boundedHTTPClient(c.HTTP)
	response, err := client.Do(request)
	if err != nil {
		return result, clientBoundaryError(ctx, err, "core API unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		var problem core.ProblemError
		if err = json.NewDecoder(io.LimitReader(response.Body, MaxAPIBytes)).Decode(&problem); err != nil {
			return passbooking.ExportSnapshot{}, clientBoundaryError(ctx, err, "invalid export response")
		}
		problem.Status = response.StatusCode
		return result, &problem
	}
	const maxEnvelopeBytes = passbooking.MaxExportBytes*4/3 + passbooking.MaxExportAuthorityBytes
	body, err := io.ReadAll(io.LimitReader(response.Body, maxEnvelopeBytes+1))
	if err != nil {
		return result, clientBoundaryError(ctx, err, "export response unavailable")
	}
	if len(body) > maxEnvelopeBytes {
		return result, errors.New("invalid export size")
	}
	if err = json.Unmarshal(body, &result); err != nil {
		return passbooking.ExportSnapshot{}, clientBoundaryError(ctx, err, "invalid export response")
	}
	if len(result.Body) == 0 || len(result.Body) > passbooking.MaxExportBytes ||
		!passbooking.ValidExportEvents(result.Events) {
		return passbooking.ExportSnapshot{}, errors.New("invalid export snapshot")
	}
	return result, nil
}

func (c Host) CheckPassExportSnapshot(ctx context.Context, owner string, events []string) error {
	if c.UserToken == nil {
		return identity.ErrZitadelIdentity
	}
	if !passbooking.ValidExportEvents(events) {
		return errors.New("invalid export snapshot")
	}
	if c.LocalDerived != nil {
		return c.localCheckPassExportSnapshot(ctx, owner, events)
	}
	token, err := c.UserToken(ctx, owner)
	if err != nil {
		return err
	}
	body, err := json.Marshal(struct {
		Events []string `json:"events"`
	}{Events: events})
	if err != nil {
		return err
	}
	var result struct{}
	return requestAuthorized(
		ctx,
		c.Base,
		c.HTTP,
		token,
		c.Signer.DerivedMutationToken(owner),
		http.MethodPost,
		"/internal/passes/export-authority",
		body,
		&result,
	)
}
