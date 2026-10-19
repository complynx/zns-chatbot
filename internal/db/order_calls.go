package db

import (
	"github.com/google/uuid"

	"github.com/complynx/zns-chatbot/internal/orders"
)

func orderPrepareCall(operation uuid.UUID, args OrderSubmission, source OrderSource) (orderCall, error) {
	if operation == uuid.Nil || !args.valid() || !source.valid() {
		return orderCall{}, orders.ErrSelection
	}
	hash, err := orderHash(args)
	return orderCall{
		event:        args.EventID,
		operation:    operation,
		kind:         OrderPrepare,
		source:       source,
		businessHash: hash,
		requestHash:  hash,
	}, err
}

func orderCancelCall(operation uuid.UUID, args OrderCancellation, source OrderSource) (orderCall, error) {
	if operation == uuid.Nil || args.EventID == uuid.Nil || args.OrderID == uuid.Nil || args.ExpectedVersion < 1 ||
		!source.valid() {
		return orderCall{}, orders.ErrSelection
	}
	hash, err := orderHash(args)
	return orderCall{
		event:        args.EventID,
		operation:    operation,
		kind:         OrderCancel,
		source:       source,
		businessHash: hash,
		requestHash:  hash,
	}, err
}
