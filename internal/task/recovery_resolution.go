package task

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/loom"
)

// ReconcileRecovery is an explicit operator retry of the original accepted
// request. It may deliver work, but never renews consent or withdraws a message.
func (s *Service) ReconcileRecovery(ctx context.Context, in db.CloseTaskRequest) (db.TaskResolution, error) {
	return s.resolveRecovery(ctx, in, false)
}

// AbandonRecovery requires an explicit operator request and a known retained message.
func (s *Service) AbandonRecovery(ctx context.Context, in db.CloseTaskRequest) (db.TaskResolution, error) {
	return s.resolveRecovery(ctx, in, true)
}

type recoveryWithdrawal struct {
	OperationID     string `json:"operation_id"`
	Actor           string `json:"actor"`
	Reason          string `json:"reason"`
	ExpectedVersion int64  `json:"expected_version"`
}

func withdrawalRequest(in db.CloseTaskRequest) recoveryWithdrawal {
	return recoveryWithdrawal{in.OperationID, in.Actor, in.Reason, in.ExpectedVersion}
}

func (s *Service) resolveRecovery(
	ctx context.Context,
	in db.CloseTaskRequest,
	withdraw bool,
) (db.TaskResolution, error) {
	diagnostic, err := s.store.TaskDiagnostics(ctx, in.TaskID, in.BotID, in.Organization)
	if err != nil {
		return db.TaskResolution{}, err
	}
	record := in
	record.OperationID = db.RecoveryReconciliationPrefix + in.OperationID
	if withdraw {
		record.OperationID = db.RecoveryAbandonmentPrefix + in.OperationID
	}
	for _, prior := range diagnostic.Resolutions {
		if prior.OperationID == record.OperationID {
			if prior.Actor != in.Actor || prior.Reason != in.Reason || prior.ExpectedVersion != in.ExpectedVersion {
				return db.TaskResolution{}, db.ErrConflict
			}
			prior.OperationID = in.OperationID
			return prior, nil
		}
	}
	owner := s.owner + ":reconcile"
	t, err := s.store.Claim(ctx, in.TaskID, owner, s.options.LeaseDuration)
	if err != nil {
		return db.TaskResolution{}, err
	}
	defer func() {
		releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
		defer cancel()
		_ = s.store.Release(releaseCtx, t.ID, owner, t.Fence)
	}()
	var c checkpoint
	var i intent
	if json.Unmarshal(t.Snapshot, &c) != nil || json.Unmarshal(t.Request, &i) != nil ||
		!canResolveRecovery(t, in, c.Recovery, withdraw) {
		return db.TaskResolution{}, db.ErrConflict
	}
	receipt, err := s.requestRecoveryResolution(ctx, &t, &c, i, owner, in, withdraw)
	if err != nil {
		return db.TaskResolution{}, err
	}
	if withdraw {
		c.Recovery.Receipt = &receipt
		t.Snapshot, err = json.Marshal(c)
		if err != nil {
			return db.TaskResolution{}, err
		}
		result, saveErr := s.store.SaveRecoveryWithdrawal(ctx, t, owner, record)
		result.OperationID = in.OperationID
		return result, saveErr
	}
	return s.saveReconciledRecovery(ctx, t, c, owner, record, receipt)
}

func (s *Service) requestRecoveryResolution(ctx context.Context, t *db.Task, c *checkpoint, i intent,
	owner string, in db.CloseTaskRequest, withdraw bool) (loom.RecoveryReceipt, error) {
	user, service, err := s.authorize(ctx, *t, i)
	if err != nil {
		return loom.RecoveryReceipt{}, err
	}
	credential, err := s.iam.UserToken(ctx, user.Subject)
	if err != nil {
		return loom.RecoveryReceipt{}, err
	}
	auth := loom.Auth{ServiceToken: service.Token, SubjectToken: credential.Token}
	if withdraw {
		if err = s.prepareRecoveryWithdrawal(ctx, t, c, owner, in); err != nil {
			return loom.RecoveryReceipt{}, err
		}
		receipt, abandonErr := s.loom.AbandonMessage(ctx, auth, c.Recovery.ChatID, c.Recovery.Receipt.MessageID,
			uuid.NewSHA1(t.ID, []byte("abandon:"+in.OperationID)).String())
		if upstream, known := errors.AsType[*loom.Error](abandonErr); known && upstream.Outcome == loom.Rejected &&
			upstream.Code == "message_already_delivered" {
			// Positive delivery evidence permits normal receipt replay/observation,
			// never local closure. Other failures keep the withdrawal intent frozen.
			c.Recovery.Withdrawal = nil
			c.Recovery.RetryUntil = time.Now().Add(recoveryRetryWindow)
			if saveErr := s.saveRecoveryResolution(ctx, t, *c, owner); saveErr != nil {
				return loom.RecoveryReceipt{}, saveErr
			}
		}
		return receipt, abandonErr
	}
	return s.replayRecoveryResolution(ctx, t, c, owner, auth)
}

func (s *Service) replayRecoveryResolution(ctx context.Context, t *db.Task, c *checkpoint,
	owner string, auth loom.Auth) (loom.RecoveryReceipt, error) {
	unsubmitted := c.Recovery.Unsubmitted
	if unsubmitted {
		c.Recovery.Unsubmitted = false
		if err := s.saveRecoveryResolution(ctx, t, *c, owner); err != nil {
			return loom.RecoveryReceipt{}, err
		}
	}
	receipt, err := s.loom.RecoverMessage(ctx, auth, c.Recovery.ChatID, t.ID.String(), c.Recovery.Request)
	if err != nil {
		if unsubmitted && recoveryAttemptNotAccepted(err) {
			c.Recovery.Unsubmitted = true
			if saveErr := s.saveRecoveryResolution(ctx, t, *c, owner); saveErr != nil {
				return loom.RecoveryReceipt{}, saveErr
			}
		}
		return loom.RecoveryReceipt{}, err
	}
	if !receipt.Accepted || receipt.MessageID == "" ||
		c.Recovery.Receipt != nil && c.Recovery.Receipt.MessageID != "" &&
			c.Recovery.Receipt.MessageID != receipt.MessageID {
		return loom.RecoveryReceipt{}, errors.New("recovery receipt identity not confirmed")
	}
	return receipt, nil
}

func canResolveRecovery(t db.Task, in db.CloseTaskRequest, r *messageRecovery, withdraw bool) bool {
	if !withdraw {
		return unknownRecoveryState(t.State) && t.Version == in.ExpectedVersion && canReconcileInbox(t.State, r)
	}
	if t.State == stateClosed || r == nil || r.Rejected || r.Receipt == nil || !r.Receipt.Accepted ||
		r.Receipt.MessageID == "" || r.Receipt.Delivered || r.Receipt.Abandoned {
		return false
	}
	if r.Withdrawal != nil {
		return *r.Withdrawal == withdrawalRequest(in)
	}
	return t.Version == in.ExpectedVersion
}

func (s *Service) prepareRecoveryWithdrawal(ctx context.Context, t *db.Task, c *checkpoint,
	owner string, in db.CloseTaskRequest) error {
	if c.Recovery.Withdrawal != nil {
		return nil
	}
	request := withdrawalRequest(in)
	c.Recovery.Withdrawal = &request
	return s.saveRecoveryResolution(ctx, t, *c, owner)
}

func (s *Service) saveRecoveryResolution(ctx context.Context, t *db.Task, c checkpoint, owner string) error {
	raw, err := json.Marshal(c)
	if err != nil {
		return err
	}
	t.Snapshot = raw
	saved, err := s.store.Save(ctx, *t, owner, t.Fence, nil)
	if err == nil {
		*t = saved
	}
	return err
}

func canReconcileInbox(state string, recovery *messageRecovery) bool {
	if recovery == nil || recovery.ChatID == "" || recovery.Request.Text == "" || recovery.Rejected ||
		recovery.Withdrawal != nil {
		return false
	}
	if recovery.Receipt == nil {
		return true
	}
	return !recovery.Receipt.Abandoned && (state != stateDeliveryUnknown || !recovery.Receipt.Delivered)
}

func (s *Service) saveReconciledRecovery(ctx context.Context, t db.Task, c checkpoint, owner string,
	record db.CloseTaskRequest, receipt loom.RecoveryReceipt) (db.TaskResolution, error) {
	c.Recovery.Receipt = &receipt
	if receipt.Delivered && !receipt.Abandoned {
		// Observation uses fresh subject authorization. An unknown renewal
		// remains audit evidence, never a fabricated newer session receipt.
		c.SessionVersionUnknown = c.SessionVersionUnknown || c.Reauthorizing || t.State == stateAuthUnknown
		c.Reauthorizing = false
		t.State = stateAwaitingService
	}
	raw, err := json.Marshal(c)
	if err != nil {
		return db.TaskResolution{}, err
	}
	t.Snapshot = raw
	result, err := s.store.SaveRecoveryReconciliation(ctx, t, owner, record,
		func(ctx context.Context, tx pgx.Tx, id uuid.UUID) error {
			_, insertErr := s.queue.InsertTx(ctx, tx, JobArgs{TaskID: id}, nil)
			return insertErr
		})
	result.OperationID = strings.TrimPrefix(result.OperationID, db.RecoveryReconciliationPrefix)
	return result, err
}

func unknownRecoveryState(state string) bool {
	return state == stateAuthUnknown || state == stateDeliveryUnknown
}
