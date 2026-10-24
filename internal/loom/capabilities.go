package loom

import (
	"context"
	"errors"
	"net/http"
)

// RequireOutputValidation checks the authenticated consumer contract before a restricted run starts.
// Do not infer enforcement from successful creation on an older executor.
func (c *Client) RequireOutputValidation(ctx context.Context, auth Auth) error {
	var result struct {
		OutputValidation struct {
			Version             int  `json:"version"`
			DisallowCustomEmoji bool `json:"disallow_custom_emoji"`
		} `json:"output_validation"`
	}
	if err := c.call(ctx, http.MethodGet, "/capabilities", auth, "", nil, &result, false, false); err != nil {
		var upstream *Error
		if errors.As(err, &upstream) && upstream.StatusCode == http.StatusNotFound {
			return &Error{Code: "output_validation_unsupported", Outcome: NotStarted}
		}
		return err
	}
	if result.OutputValidation.Version != 1 || !result.OutputValidation.DisallowCustomEmoji {
		return &Error{Code: "output_validation_unsupported", Outcome: NotStarted}
	}
	return nil
}
