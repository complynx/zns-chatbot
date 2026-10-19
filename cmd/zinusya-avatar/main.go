package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/complynx/zns-chatbot/internal/avatar"
	"github.com/complynx/zns-chatbot/internal/avatar/renderer"
)

func main() { os.Exit(run()) }

func run() int {
	raw, transform, err := avatar.ReadInput(os.Stdin)
	if errors.Is(err, avatar.ErrImageLimit) {
		return fail("avatar input exceeds limit", avatar.ExitImageLimit)
	}
	if err != nil {
		return fail("avatar input rejected", avatar.ExitInvalidImage)
	}
	result, err := renderer.Render(context.Background(), raw, transform)
	if errors.Is(err, avatar.ErrInvalidImage) || errors.Is(err, avatar.ErrInvalidTransform) {
		return fail("avatar image rejected", avatar.ExitInvalidImage)
	}
	if errors.Is(err, avatar.ErrImageLimit) {
		return fail("avatar image exceeds limit", avatar.ExitImageLimit)
	}
	if err != nil {
		return fail("avatar rendering failed", 1)
	}
	if json.NewEncoder(os.Stdout).Encode(result) != nil {
		return fail("avatar output unavailable", 1)
	}
	return 0
}

func fail(message string, code int) int {
	_, _ = fmt.Fprintln(os.Stderr, message)
	return code
}
