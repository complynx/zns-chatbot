package delivery

import (
	"errors"
	"strconv"
	"time"
)

var ErrQueueReference = errors.New("invalid delivery queue reference")
var ErrQueueBinding = errors.New("delivery queue binding changed")
var ErrQueueState = errors.New("delivery queue state conflict")

// Owner identifies an existing outbox owner, never an arbitrary table or method.
type Owner string

const (
	Orders       Owner = "orders"
	Passes       Owner = "passes"
	Food         Owner = "food"
	Massage      Owner = "massage"
	Admin        Owner = "admin"
	Announcement Owner = "announcement"
	Bot          Owner = "bot"
)

type Class string

const (
	Interactive Class = "interactive"
	Background  Class = "background"
)

// Sending is an index projection, not a completed wire outcome.
const Sending Kind = "sending"

// Reference binds one transport effect to a durable operation owned elsewhere.
type Reference struct {
	Owner  Owner
	Key    string
	Effect string
}

func (r Reference) valid() bool {
	if len(r.Key) == 0 || len(r.Key) > 200 || len(r.Effect) == 0 || len(r.Effect) > 100 {
		return false
	}
	switch r.Owner {
	case Orders, Passes, Food, Massage, Admin, Announcement, Bot:
		return true
	default:
		return false
	}
}

// Entry is a scheduling projection. Attempt generations and leases belong to its owner.
type Entry struct {
	Reference   Reference
	Destination Destination
	Sequence    int64
	Class       Class
	State       Kind
	NotBefore   time.Time
}

func validDestination(destination Destination) bool {
	chat, err := strconv.ParseInt(destination.Chat, 10, 64)
	return err == nil && chat != 0 && strconv.FormatInt(chat, 10) == destination.Chat && destination.Thread >= 0
}

func terminal(state Kind) bool {
	return state == Succeeded || state == Rejected || state == Cancelled
}
