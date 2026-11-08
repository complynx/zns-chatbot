package loom

import (
	"context"
	"net/http"
)

// CreditBalance is the caller's own wallet. Available already excludes reservations.
type CreditBalance struct {
	AvailableMicrocredits int64
	ReservedMicrocredits  int64
}

// OwnCredits reads only the authenticated machine's wallet, without delegation.
func (c *Client) OwnCredits(ctx context.Context, token string) (CreditBalance, error) {
	var reply struct {
		Available *int64 `json:"available_microcredits"`
		Reserved  *int64 `json:"reserved_microcredits"`
	}
	err := c.call(ctx, http.MethodGet, "/users/me/credits", Auth{ServiceToken: token}, "", nil, &reply, false, false)
	if err != nil {
		return CreditBalance{}, err
	}
	if reply.Available == nil || reply.Reserved == nil || *reply.Reserved < 0 {
		return CreditBalance{}, &Error{Outcome: Rejected}
	}
	return CreditBalance{AvailableMicrocredits: *reply.Available, ReservedMicrocredits: *reply.Reserved}, nil
}
