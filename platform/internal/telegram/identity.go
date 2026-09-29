package telegram

import (
	"context"
	"errors"
)

// VerifyBot checks the token's bot namespace before runtime starts polling.
// Provider responses and token-bearing request URLs must not enter startup errors.
func (c Client) VerifyBot(ctx context.Context, expectedID int64) error {
	if expectedID <= 0 {
		return errors.New("telegram bot identity requires a positive configured ID")
	}
	var user User
	if err := c.Call(ctx, "getMe", struct{}{}, &user); err != nil {
		return errors.New("telegram bot identity unavailable")
	}
	if !user.IsBot || user.ID != expectedID {
		return errors.New("telegram bot identity does not match configuration")
	}
	return nil
}
