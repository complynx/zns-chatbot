package identity

import (
	"context"
	"fmt"
)

// CheckAccess verifies the service credentials and introspection client without
// creating users or exchanging a human token. The service JWT stays cached.
func (c *Client) CheckAccess(ctx context.Context) (Credential, error) {
	credential, err := c.ServiceToken(ctx)
	if err != nil {
		return Credential{}, fmt.Errorf("IAM service credentials: %w", err)
	}
	if _, err = c.Verify(ctx, credential.Token); err != nil {
		return Credential{}, fmt.Errorf("IAM introspection credentials: %w", err)
	}
	return credential, nil
}
