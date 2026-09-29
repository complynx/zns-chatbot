package passbooking

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBookingCommandBoundsBeforeTransaction(t *testing.T) {
	t.Parallel()
	for _, field := range []struct {
		name string
		set  func(*Command, string)
	}{
		{"name", func(c *Command, v string) { c.Name = v }},
		{"event", func(c *Command, v string) { c.Event = v }},
		{"key", func(c *Command, v string) { c.Key = v }},
		{"target", func(c *Command, v string) { c.Target = v }},
		{"payment admin", func(c *Command, v string) { c.PaymentAdmin = v }},
		{"proof", func(c *Command, v string) { c.ProofID = v }},
		{"payment attempt", func(c *Command, v string) { c.PaymentAttempt = v }},
	} {
		t.Run(field.name, func(t *testing.T) {
			t.Parallel()
			for _, value := range []string{strings.Repeat("x", 201), "bad\x00value", string([]byte{0xff})} {
				command := Command{Name: "solo", Event: "dance", Key: "bounded"}
				field.set(&command, value)
				require.Error(t, validate(command))
				_, err := (Service{}).Execute(t.Context(), "alice", command)
				require.Error(t, err, "invalid input must be rejected before accessing a nil database")
				_, err = (Service{}).PrepareInTx(t.Context(), nil, "alice", command)
				require.Error(t, err, "invalid input must be rejected before accessing the supplied transaction")
			}
		})
	}
}

func TestBookingCommandStringBoundary(t *testing.T) {
	t.Parallel()
	for _, value := range []string{strings.Repeat("x", 200), strings.Repeat("é", 100)} {
		command := Command{
			Name:           "solo",
			Event:          value,
			Key:            value,
			Target:         value,
			PaymentAdmin:   value,
			ProofID:        value,
			PaymentAttempt: value,
		}
		require.NoError(t, validate(command))
	}
	require.NoError(t, validate(Command{Name: "solo", Event: "dance", Key: "empty-optional"}))
}
