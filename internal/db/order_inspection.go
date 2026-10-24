package db

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
)

// OrderPreparationContract is supplied by the authenticated API, not request JSON.
type OrderPreparationContract struct {
	Issuer   string `json:"issuer"`
	Actor    string `json:"actor"`
	Revision string `json:"revision"`
}

type orderPreparationBinding struct {
	OrderPreparationContract

	BotID        int64  `json:"bot_id"`
	Subject      string `json:"subject"`
	Organization string `json:"org_id"`
}

// The marker belongs to the immutable operation receipt, not the shared proposal.
// Legacy decoders ignore it; public receipt reads project OrderPreparation only.
type orderPreparationDocument struct {
	OrderPreparation

	Contract *orderPreparationBinding `json:"operation_contract,omitempty"`
}

type OrderInspection struct {
	Contract              OrderPreparationContract
	Subject, Organization string
	Preparation           OrderPreparation
	Result                *OrderSubmissionResult
}

func contractedOrderPreparation(scope ProfileScope, call orderCall, p OrderPreparation) (OrderPreparation, any, error) {
	if call.contract == nil {
		return p, p, nil
	}
	c := *call.contract
	if (call.businessKind() != OrderKindService && call.businessKind() != OrderKindMeal) ||
		c.Issuer == "" || c.Actor == "" || c.Revision == "" ||
		call.describe == nil {
		return p, nil, ErrConflict
	}
	description, err := call.describe(p)
	if err != nil {
		return p, nil, err
	}
	if strings.TrimSpace(description) == "" || len(description) > 4000 || !utf8.ValidString(description) {
		return p, nil, ErrConflict
	}
	p.Description = description
	binding := &orderPreparationBinding{
		OrderPreparationContract: c,
		BotID:                    scope.BotID,
		Subject:                  scope.Subject,
		Organization:             scope.Organization,
	}
	return p, orderPreparationDocument{OrderPreparation: p, Contract: binding}, nil
}

// InspectOrderSubmissionOperation returns original terms/receipt without preparing,
// executing or renewing anything. Completed receipts still require current access.
func (s *Store) InspectOrderSubmissionOperation(ctx context.Context, scope ProfileScope,
	operation uuid.UUID, contract OrderPreparationContract,
) (OrderInspection, error) {
	return s.inspectOrderSubmissionOperation(ctx, scope, operation, contract, OrderKindService)
}

// InspectMealOrderSubmissionOperation uses meals entitlement and rejects service
// order receipts even though both kinds share the operation namespace.
func (s *Store) InspectMealOrderSubmissionOperation(ctx context.Context, scope ProfileScope,
	operation uuid.UUID, contract OrderPreparationContract,
) (OrderInspection, error) {
	return s.inspectOrderSubmissionOperation(ctx, scope, operation, contract, OrderKindMeal)
}

func (s *Store) inspectOrderSubmissionOperation(ctx context.Context, scope ProfileScope,
	operation uuid.UUID, contract OrderPreparationContract, kind OrderKind,
) (OrderInspection, error) {
	var out OrderInspection
	if operation == uuid.Nil || contract.Issuer == "" || contract.Actor == "" || contract.Revision == "" {
		return out, ErrConflict
	}
	// The existing read resolves the event with owner checks and releases its
	// connection before the normal operation -> event -> owner write lock order.
	readOperation, begin := s.OwnOrderOperationByID, s.orderTx
	if kind == OrderKindMeal {
		readOperation, begin = s.OwnMealOrderOperationByID, s.mealOrderTx
	}
	hint, err := readOperation(ctx, scope, operation, OrderPrepare)
	if err != nil {
		return out, err
	}
	tx, err := begin(ctx, scope, hint.EventID, operation)
	if err != nil {
		return out, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	rows, err := s.q.WithTx(tx).
		ReadOrderOperations(ctx, sqlc.ReadOrderOperationsParams{UserID: scope.UserID, OperationID: operation})
	if err != nil {
		return out, err
	}
	var prepared, submitted *sqlc.ShuttleOrderOperation
	for i := range rows {
		row := &rows[i]
		if row.OrderKind != string(kind) || row.EventID != hint.EventID {
			return out, ErrConflict
		}
		switch OrderOperationKind(row.Kind) {
		case OrderPrepare:
			prepared = row
		case OrderSubmit:
			submitted = row
		case OrderCancel:
			return out, ErrConflict
		default:
			return out, ErrConflict
		}
	}
	if prepared == nil {
		return out, pgx.ErrNoRows
	}
	p, err := inspectOrderPreparation(scope, *prepared, contract)
	if err != nil {
		return out, err
	}
	out = OrderInspection{Contract: contract, Subject: scope.Subject, Organization: scope.Organization, Preparation: p}
	if submitted != nil {
		result, receiptErr := inspectOrderSubmissionReceipt(*submitted, p)
		if receiptErr != nil {
			return OrderInspection{}, receiptErr
		}
		out.Result = &result
	}
	return out, nil
}

func inspectOrderPreparation(
	scope ProfileScope,
	row sqlc.ShuttleOrderOperation,
	c OrderPreparationContract,
) (OrderPreparation, error) {
	var doc orderPreparationDocument
	if err := json.Unmarshal(row.Result, &doc); err != nil {
		return OrderPreparation{}, err
	}
	b := doc.Contract
	p := doc.OrderPreparation
	if b == nil || b.OrderPreparationContract != c || b.BotID != scope.BotID || b.Subject != scope.Subject ||
		b.Organization != scope.Organization {
		return OrderPreparation{}, ErrConflict
	}
	hash, err := orderHash(p.Arguments)
	if err != nil {
		return OrderPreparation{}, err
	}
	if p.ID == uuid.Nil || !row.PreparationID.Valid || row.PreparationID.Bytes != [16]byte(p.ID) ||
		p.Arguments.EventID != row.EventID ||
		!bytes.Equal(row.BusinessHash, hash) ||
		!bytes.Equal(row.RequestHash, hash) ||
		strings.TrimSpace(p.Description) == "" ||
		len(p.Description) > 4000 ||
		p.ExpiresAt.IsZero() {
		return OrderPreparation{}, ErrConflict
	}
	return p, nil
}

func inspectOrderSubmissionReceipt(row sqlc.ShuttleOrderOperation, p OrderPreparation) (OrderSubmissionResult, error) {
	var result OrderSubmissionResult
	call, err := orderSubmitCall(row.OperationID, p.Arguments, p.ID, OrderSourceAgent)
	if err != nil {
		return result, err
	}
	if !row.PreparationID.Valid || row.PreparationID.Bytes != [16]byte(p.ID) || !row.OrderID.Valid ||
		!bytes.Equal(row.BusinessHash, call.businessHash) || !bytes.Equal(row.RequestHash, call.requestHash) {
		return result, ErrConflict
	}
	if err = json.Unmarshal(row.Result, &result); err != nil {
		return result, err
	}
	if result.Order.ID != uuid.UUID(row.OrderID.Bytes) || result.Order.EventID != p.Arguments.EventID ||
		result.Order.SourceDraftVersion != p.Arguments.ExpectedDraftVersion {
		return result, ErrConflict
	}
	same, err := sameOrderConditions(result.Order.Snapshot, p.Snapshot)
	if err != nil {
		return result, err
	}
	if !same {
		return result, ErrConflict
	}
	return result, nil
}
