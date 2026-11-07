package db

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
	"github.com/complynx/zns-chatbot/internal/payments"
)

const avatarProposalPageBytes = 48 * 1024
const MaxAvatarDeliveryProposalPageSize = 20
const avatarProposalExpired = "expired"
const avatarProposalCompleted = "completed"

// AvatarDeliveryProposalView preserves the original proposal, separate from its result.
// Pending or expired proposals are not permission to retry a delivery.
type AvatarDeliveryProposalView struct {
	AvatarDeliveryPreparation

	OperationID uuid.UUID                       `json:"operation_id"`
	State       string                          `json:"state"            enum:"pending,expired,completed"`
	Result      *AvatarDeliveryResolutionResult `json:"result,omitempty"`
}

type AvatarDeliveryProposalSelector struct {
	OperationID uuid.UUID `json:"operation_id,omitempty"`
	ProposalID  uuid.UUID `json:"proposal_id,omitempty"`
}

type AvatarDeliveryProposalPage struct {
	Items      []AvatarDeliveryProposalView `json:"items"`
	NextCursor *uuid.UUID                   `json:"next_cursor,omitempty"`
}

func validateAvatarProposalScope(scope AvatarDeliveryAdminScope) error {
	if !scope.GlobalAdmin {
		return payments.ErrDenied
	}
	if scope.BotID <= 0 || scope.Organization == "" || scope.Subject == "" || scope.UserID == uuid.Nil {
		return ErrInvalidAvatar
	}
	return nil
}

func avatarProposalView(row sqlc.ShuttleAvatarDeliveryResolution, now time.Time) (AvatarDeliveryProposalView, error) {
	view := AvatarDeliveryProposalView{OperationID: row.OperationID, State: "pending"}
	if err := json.Unmarshal(row.Document, &view.AvatarDeliveryPreparation); err != nil {
		return view, err
	}
	view.ExpiresAt = row.ExpiresAt.Time.UTC()
	if !now.Before(view.ExpiresAt) {
		view.State = avatarProposalExpired
	}
	if len(row.Result) != 0 {
		view.Result = &AvatarDeliveryResolutionResult{}
		if err := json.Unmarshal(row.Result, view.Result); err != nil {
			return view, err
		}
		view.State = avatarProposalCompleted
	}
	return view, nil
}

// AvatarDeliveryProposal reads exactly one actor-owned original proposal, including expired or completed ones.
func (s *Store) AvatarDeliveryProposal(
	ctx context.Context,
	scope AvatarDeliveryAdminScope,
	selector AvatarDeliveryProposalSelector,
) (AvatarDeliveryProposalView, error) {
	return s.avatarDeliveryProposal(ctx, scope, selector, nil)
}

func (s *Store) avatarDeliveryProposal(
	ctx context.Context,
	scope AvatarDeliveryAdminScope,
	selector AvatarDeliveryProposalSelector,
	session *MiniAppAvatarAdminSession,
) (AvatarDeliveryProposalView, error) {
	if err := validateAvatarProposalScope(scope); err != nil {
		return AvatarDeliveryProposalView{}, err
	}
	if (selector.OperationID == uuid.Nil) == (selector.ProposalID == uuid.Nil) {
		return AvatarDeliveryProposalView{}, ErrInvalidAvatar
	}
	key := selector.OperationID
	if key == uuid.Nil {
		key = selector.ProposalID
	}
	tx, q, err := s.beginAvatarResolutionSession(ctx, scope, key, session)
	if err != nil {
		return AvatarDeliveryProposalView{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	row, err := q.GetAvatarDeliveryProposal(
		ctx,
		sqlc.GetAvatarDeliveryProposalParams{
			BotID:          scope.BotID,
			OrganizationID: scope.Organization,
			Actor:          scope.Subject,
			OperationID:    selector.OperationID,
			ProposalID:     selector.ProposalID,
		},
	)
	if err != nil {
		return AvatarDeliveryProposalView{}, err
	}
	now, err := q.RegistrationNow(ctx)
	if err != nil {
		return AvatarDeliveryProposalView{}, err
	}
	return avatarProposalView(row, now.Time)
}

// PendingAvatarDeliveryProposals uses an exclusive proposal UUID cursor, not an offset.
// The byte budget may shorten a page; the cursor always follows its last returned item.
func (s *Store) PendingAvatarDeliveryProposals(
	ctx context.Context,
	scope AvatarDeliveryAdminScope,
	after uuid.UUID,
	limit int,
) (AvatarDeliveryProposalPage, error) {
	return s.pendingAvatarDeliveryProposals(ctx, scope, after, limit, nil)
}

func (s *Store) pendingAvatarDeliveryProposals(
	ctx context.Context,
	scope AvatarDeliveryAdminScope,
	after uuid.UUID,
	limit int,
	session *MiniAppAvatarAdminSession,
) (AvatarDeliveryProposalPage, error) {
	page := AvatarDeliveryProposalPage{Items: []AvatarDeliveryProposalView{}}
	if err := validateAvatarProposalScope(scope); err != nil {
		return page, err
	}
	if limit == 0 {
		limit = MaxAvatarDeliveryProposalPageSize
	}
	if limit < 1 || limit > MaxAvatarDeliveryProposalPageSize {
		return page, ErrInvalidAvatar
	}
	tx, q, err := s.beginAvatarResolutionSession(ctx, scope, uuid.Nil, session)
	if err != nil {
		return page, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	now, err := q.RegistrationNow(ctx)
	if err != nil {
		return page, err
	}
	rows, err := q.ListPendingAvatarDeliveryProposals(
		ctx,
		sqlc.ListPendingAvatarDeliveryProposalsParams{
			BotID:          scope.BotID,
			OrganizationID: scope.Organization,
			Actor:          scope.Subject,
			AfterID:        after,
			NowAt:          now,
			PageLimit:      int32(limit + 1),
		},
	)
	if err != nil {
		return page, err
	}
	size := 0
	for _, row := range rows {
		view, parseErr := avatarProposalView(row, now.Time)
		if parseErr != nil {
			return page, parseErr
		}
		raw, marshalErr := json.Marshal(view)
		if marshalErr != nil {
			return page, marshalErr
		}
		if len(page.Items) == limit || size+len(raw) > avatarProposalPageBytes {
			if len(page.Items) == 0 {
				return page, ErrInvalidAvatar
			}
			last := page.Items[len(page.Items)-1].ID
			page.NextCursor = &last
			break
		}
		page.Items = append(page.Items, view)
		size += len(raw)
	}
	return page, nil
}
