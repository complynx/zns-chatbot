package credits

import (
	"context"
	"sync"
)

type receiptKey struct{}

const MaxRemoteReceipts = 32

type ReceiptCollector struct {
	mu  sync.Mutex
	ids []string
}

func WithReceiptCollector(ctx context.Context) (context.Context, *ReceiptCollector) {
	collector := &ReceiptCollector{}
	return context.WithValue(ctx, receiptKey{}, collector), collector
}

func (c *ReceiptCollector) IDs() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string{}, c.ids...)
}

func collectReceipt(ctx context.Context, id string) {
	if c, ok := ctx.Value(receiptKey{}).(*ReceiptCollector); ok {
		c.mu.Lock()
		defer c.mu.Unlock()
		c.ids = append(c.ids, id)
	}
}

type RemoteReceiptVerifier interface {
	VerifyRemoteReceipts(context.Context, []string) error
}

// VerifyRemoteReceipts correlates trusted remote receipts to the original host
// scope. It never creates another attempt or accepts remote monetary values.
func (s Service) VerifyRemoteReceipts(ctx context.Context, ids []string) error {
	if len(ids) > MaxRemoteReceipts {
		return ErrInvalid
	}
	scope := ScopeFromContext(ctx)
	for _, id := range ids {
		var exists bool
		err := s.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM credits.attempts WHERE id::text=$1 AND actor=$2 AND payer=$3 AND operation_key=$4)`,
			id, scope.Actor, scope.Payer, scope.Key).
			Scan(&exists)
		if err != nil {
			return err
		}
		if !exists {
			return ErrConflict
		}
	}
	return nil
}
