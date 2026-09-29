package appclient

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

func (c Client) ExportPasses(ctx context.Context, owner string) ([]byte, error) {
	if c.LocalRegistration != nil {
		return c.localExportPasses(ctx, owner)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.Base+"/v1/passes/export", http.NoBody)
	if err != nil {
		return nil, err
	}
	token, err := c.UserToken(ctx, owner)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+token)
	client := c.HTTPClient()
	response, err := client.Do(request)
	if err != nil {
		return nil, clientBoundaryError(ctx, err, "core API unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		var problem core.ProblemError
		if err = json.NewDecoder(io.LimitReader(response.Body, MaxAPIBytes)).Decode(&problem); err != nil {
			return nil, err
		}
		problem.Status = response.StatusCode
		return nil, &problem
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, passbooking.MaxExportBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) == 0 || len(body) > passbooking.MaxExportBytes {
		return nil, errors.New("invalid export size")
	}
	return body, nil
}
