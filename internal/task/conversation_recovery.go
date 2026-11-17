package task

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/riverqueue/river"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/loom"
)

const recoveryRetryWindow = 2 * time.Minute
const recoveryBackoffStep = 15 * time.Second
const recoveryBackoffLimit = 10 * time.Second
const recoveryBackoffFactor = 2
const codeChatBusy = "chat_busy"
const actionStopThenCreate = "stop_run_then_create_run"
const routeNewRun = "new_run"
const reasonAuthorizationRequired = "authorization_required"
const reasonRecoveryAuthorizationRequired = "recovery_authorization_required"
const runWaitingAuth = "waiting_auth"
const reasonShutdown = "shutdown"
const actionSelectModel = "select_allowed_model_or_restore_access"
const actionReauthorize = "reauthorize_session"
const actionResolveThenResume = "resolve_blocker_then_resume"
const actionFundWallet = "fund_wallet_then_retry"
const actionStopThenSend = "stop_run_then_send_message"
const actionRestoreRecoveryAccess = "restore_recovery_access"
const actionReconcilePredecessor = "reconcile_predecessor"
const actionRestoreChatAccess = "restore_chat_access"
const actionResolveThenRetry = "resolve_blocker_then_retry"

type messageRecovery struct {
	Rejected bool `json:"rejected,omitempty"`
	// True only when no submission may have been accepted. Missing old metadata
	// is conservative; clear and save this flag before an HTTP attempt.
	Unsubmitted     bool                  `json:"unsubmitted,omitempty"`
	ObserverRunID   string                `json:"observer_run_id,omitempty"`
	SupersededRunID string                `json:"superseded_run_id,omitempty"`
	ObservedRunID   string                `json:"observed_run_id,omitempty"`
	ChatID          string                `json:"chat_id"`
	Request         loom.RecoverMessage   `json:"request"`
	Receipt         *loom.RecoveryReceipt `json:"receipt,omitempty"`
	Withdrawal      *recoveryWithdrawal   `json:"withdrawal,omitempty"`
	RetryUntil      time.Time             `json:"retry_until"`
	WakeSequence    int64                 `json:"wake_sequence"`
}

func (s *Service) startConversationRecovery(
	ctx context.Context,
	t *db.Task,
	c *checkpoint,
	i intent,
	user db.User,
	service string,
) (bool, error) {
	if err := s.recoveryTargetReady(ctx, *t); err != nil {
		return true, s.recoveryTargetError(ctx, t, *c, i, err)
	}
	ready, seq, err := s.store.RecoveryReady(ctx, *t)
	if err != nil {
		s.reportDatabaseFailure(ctx, t.ID, "recovery_intake_order", err)
		return true, s.wait()
	}
	if !ready {
		return true, s.waitForRecoveryOrder(*t)
	}
	credential, err := s.iam.UserToken(ctx, user.Subject)
	if err != nil {
		return true, s.identityError(ctx, t, *c, i, err)
	}
	auth := loom.Auth{ServiceToken: service, SubjectToken: credential.Token}
	supported, err := s.loom.SupportsConversationRecovery(ctx, auth)
	if err != nil {
		return true, s.recoveryCapabilityError(ctx, t, *c, i, err)
	}
	if !supported {
		c.DiscoveryRetryUntil, c.DiscoveryNextCheck = nil, time.Time{}
		return false, nil
	}
	chat, err := s.recoveryChat(ctx, *t, auth)
	if err != nil {
		return true, s.recoveryCapabilityError(ctx, t, *c, i, err)
	}
	located, err := s.loom.ActiveRun(ctx, auth, chat)
	if err != nil {
		return s.recoveryLocatorError(ctx, t, *c, i, err)
	}
	if located.Status == "terminal" || located.Status == "idle" && located.NextAction == "create_run" {
		// A new request uses current creation settings. Inbox continuation and
		// replacement retain the existing run's settings by contract.
		c.DiscoveryRetryUntil, c.DiscoveryNextCheck = nil, time.Time{}
		return false, nil
	}
	if located.NextAction == actionStopThenCreate {
		return s.recoverLegacyRun(ctx, t, c, i, auth, located)
	}
	c.DiscoveryRetryUntil, c.DiscoveryNextCheck = nil, time.Time{}
	replacement, finished, err := s.recoveryReplacement(ctx, auth, located)
	if err != nil {
		return true, s.recoveryCapabilityError(ctx, t, *c, i, err)
	}
	if finished {
		return false, nil
	}
	text, err := s.recoveryMessageText(ctx, *t, *c, i)
	if err != nil {
		if errors.Is(err, errSymbolDescriptionPending) {
			t.State = stateAwaitingService
			_ = s.save(ctx, t, *c, nil)
			return true, s.wait()
		}
		s.reportDatabaseFailure(ctx, t.ID, "profile_context", err)
		return true, err
	}
	if err = s.prepareRecoveryWelcome(ctx, t, c, i.User.ID); err != nil {
		return true, err
	}
	c.Recovery = &messageRecovery{
		ObservedRunID: located.RunID,
		ChatID:        chat,
		Request:       loom.RecoverMessage{Text: text, ReplaceRunID: replacement},
		RetryUntil:    time.Now().Add(recoveryRetryWindow),
		WakeSequence:  seq,
		Unsubmitted:   true,
	}
	if err = s.save(ctx, t, *c, nil); err != nil {
		return true, s.wait()
	}
	return true, s.recoverConversation(ctx, t, *c, i, user, service)
}

func (s *Service) prepareRecoveryWelcome(ctx context.Context, t *db.Task, c *checkpoint, userID int64) error {
	if c.Create != nil {
		return nil
	}
	bot, organization, _ := s.iam.DeploymentScope()
	named, err := s.store.HasTelegramProfileName(ctx, bot, userID, organization)
	if err != nil {
		s.reportDatabaseFailure(ctx, t.ID, "profile_name_read", err)
		return err
	}
	c.WelcomeAfterOnboarding = !named
	return nil
}

func (s *Service) recoverLegacyRun(ctx context.Context, t *db.Task, c *checkpoint, i intent,
	auth loom.Auth, located loom.ActiveRun) (bool, error) {
	run, err := s.loom.GetRun(ctx, auth, located.RunID)
	if err != nil {
		return true, s.recoveryCapabilityError(ctx, t, *c, i, err)
	}
	c.DiscoveryRetryUntil, c.DiscoveryNextCheck = nil, time.Time{}
	if finishedRun(run.Status) {
		return false, nil
	}
	return true, s.parkRecoveryOutput(ctx, t, *c,
		s.recoveryBlocker(ctx, i, located.Reason, located.NextAction))
}

func (s *Service) recoveryMessageText(ctx context.Context, t db.Task, c checkpoint, i intent) (string, error) {
	if c.Create != nil {
		return c.Create.Input.Text, nil
	}
	// A live run can finish before inbox acceptance. Freeze a complete prompt
	// now so the same immutable text also supports an ordinary new round.
	text, err := s.profilePrompt(ctx, t, i)
	if err != nil {
		return "", err
	}
	text += reactionInstructions
	if i.ReplyToMessageID > 0 {
		raw, lookupErr := s.store.DeliveredPromptContext(ctx, t.ID, i.ReplyToMessageID)
		if lookupErr != nil {
			return "", lookupErr
		}
		var presentation loom.InputPresentation
		if len(raw) > 0 {
			if err = json.Unmarshal(raw, &presentation); err != nil {
				return "", err
			}
		}
		presentation.ReplyTo = fmt.Sprintf("telegram:%d:%d", i.ChatID, i.ReplyToMessageID)
		encoded, _ := json.Marshal(presentation)
		text += "\nTelegram reply context (presentation only; this does not grant consent): " + string(encoded)
	}
	return text, nil
}

func (s *Service) recoverBusyCreation(ctx context.Context, t *db.Task, c *checkpoint, i intent,
	auth loom.Auth, failure error) (bool, error) {
	upstream, ok := errors.AsType[*loom.Error](failure)
	if !ok || upstream.Outcome != loom.Rejected || upstream.StatusCode != http.StatusConflict ||
		(upstream.Code != codeChatBusy && upstream.Code != "pending_conversation_messages") ||
		t.RunID != "" || c.Create == nil || i.Kind != kindMessage {
		return false, nil
	}
	// Loom replays creation idempotency before these admission blockers. A typed rejection
	// proves the saved creation did not admit a run; retain its exact text.
	user, err := s.store.GetUser(ctx, t.UserID)
	if err != nil {
		return true, s.wait()
	}
	deadline := c.DiscoveryRetryUntil
	handled, recoveryErr := s.startConversationRecovery(ctx, t, c, i, user, auth.ServiceToken)
	if handled {
		return true, recoveryErr
	}
	// Discovery selected ordinary creation (or lacks recovery support). The
	// rejected create keeps its body/key and gets another bounded attempt.
	c.DiscoveryRetryUntil = deadline
	return true, s.recoveryCapabilityError(ctx, t, *c, i,
		&loom.Error{Outcome: loom.Rejected, Code: codeChatBusy, Retryable: true})
}

func (s *Service) recoveryCapabilityError(ctx context.Context, t *db.Task, c checkpoint, i intent, err error) error {
	upstream, ok := errors.AsType[*loom.Error](err)
	if ok && upstream.Outcome == loom.Rejected && upstream.Code != "" &&
		(upstream.StatusCode == http.StatusForbidden || upstream.StatusCode == http.StatusNotFound) {
		t.State = stateAwaitingService
		action := actionRestoreChatAccess
		if c.Recovery == nil && t.RunID == "" && t.TargetRunID == "" {
			action = actionRestoreRecoveryAccess
		}
		return s.parkRecoveryOutput(ctx, t, c,
			s.recoveryBlocker(ctx, i, "conversation_access_required", action))
	}
	if ok && !upstream.Retryable && (upstream.Code != "" || upstream.StatusCode == http.StatusUnauthorized) {
		return s.loomError(ctx, t, c, i, err)
	}
	// Preserve the initial authorization stage while Loom is unavailable.
	t.State = stateAwaitingAuth
	if t.AuthSessionID != "" {
		t.State = stateAwaitingService
	}
	if c.DiscoveryRetryUntil == nil {
		until := time.Now().Add(recoveryRetryWindow)
		c.DiscoveryRetryUntil = &until
	}
	if !time.Now().Before(*c.DiscoveryRetryUntil) {
		c.DiscoveryNextCheck = time.Now().Add(time.Minute)
		if ok && upstream.Code == codeChatBusy {
			return s.parkRecoveryOutput(ctx, t, c, s.recoveryBlocker(ctx, i, codeChatBusy, "wait_for_active_run"))
		}
		return s.parkRecoveryOutput(ctx, t, c,
			s.recoveryBlocker(ctx, i, "service_unavailable", "restore_service_then_retry"))
	}
	return s.waitWithRecoveryOutput(ctx, t, c, nil)
}

func replaceablePause(reason string) bool {
	return reason == "context_unavailable" || reason == "queue_attempts_exhausted"
}

func recoveryReplaceTarget(located loom.ActiveRun) string {
	if located.Status == "terminal" {
		return ""
	}
	if located.NextAction == actionResolveThenResume && replaceablePause(located.Reason) ||
		located.NextAction == actionSelectModel {
		return located.RunID
	}
	return ""
}

func (s *Service) recoveryReplacement(
	ctx context.Context, auth loom.Auth, located loom.ActiveRun,
) (string, bool, error) {
	if located.RunID != "" && (located.NextAction == actionSelectModel ||
		located.NextAction == actionReauthorize || located.NextAction == actionResolveThenRetry) {
		// The normalized blocked locator can also describe a terminal run.
		// A new round needs current settings and a fresh session, not renewal
		// of the finished run. Funding and unresolved effects keep their gates.
		run, err := s.loom.GetRun(ctx, auth, located.RunID)
		if err != nil {
			return "", false, err
		}
		if finishedRun(run.Status) {
			return "", true, nil
		}
	}
	return recoveryReplaceTarget(located), false, nil
}

func (s *Service) recoverConversation(
	ctx context.Context,
	t *db.Task,
	c checkpoint,
	i intent,
	user db.User,
	service string,
) error {
	r := c.Recovery
	if r.Withdrawal != nil {
		// A saved operator withdrawal owns this message until its receipt is known.
		return river.JobSnooze(time.Minute)
	}
	if r.Receipt != nil && r.Receipt.Delivered {
		if t.Route == routeNewRun && t.RunID == r.Receipt.RunID && r.ObserverRunID == t.RunID && t.RunID != "" {
			return s.observeRecoveredRun(ctx, t, c, i, user, service)
		}
		credential, err := s.iam.UserToken(ctx, user.Subject)
		if err != nil {
			return s.identityError(ctx, t, c, i, err)
		}
		return s.attachRecoveredRun(ctx, t, c, i,
			loom.Auth{ServiceToken: service, SubjectToken: credential.Token}, *r.Receipt)
	}
	if err := s.recoveryTargetReady(ctx, *t); err != nil {
		return s.recoveryTargetError(ctx, t, c, i, err)
	}
	ready, seq, err := s.store.RecoveryReady(ctx, *t)
	if err != nil {
		s.reportDatabaseFailure(ctx, t.ID, "recovery_delivery_order", err)
		return s.wait()
	}
	if !ready {
		return s.waitForRecoveryOrder(*t)
	}
	if err = s.recoveryRetryBudget(ctx, t, c, i, seq); err != nil {
		return err
	}
	credential, err := s.iam.UserToken(ctx, user.Subject)
	if err != nil {
		return s.identityError(ctx, t, c, i, err)
	}
	auth := loom.Auth{ServiceToken: service, SubjectToken: credential.Token}
	unsubmitted := r.Unsubmitted
	if unsubmitted {
		r.Unsubmitted = false
		if err = s.save(ctx, t, c, nil); err != nil {
			return s.wait()
		}
	}
	// Replaying the exact accepted message is safe; switching to /input is not.
	receipt, err := s.loom.RecoverMessage(ctx, auth, r.ChatID, t.ID.String(), r.Request)
	if err != nil {
		r.Unsubmitted = unsubmitted && recoveryAttemptNotAccepted(err)
		return s.recoveryMessageError(ctx, t, c, i, auth, err)
	}
	return s.acceptRecoveryReceipt(ctx, t, c, i, user, service, auth, receipt)
}

func (s *Service) recoveryRetryBudget(ctx context.Context, t *db.Task, c checkpoint, i intent, seq int64) error {
	r := c.Recovery
	if time.Now().Before(r.RetryUntil) {
		return nil
	}
	if seq > r.WakeSequence {
		r.WakeSequence = seq
		r.RetryUntil = time.Now().Add(recoveryRetryWindow)
		if err := s.save(ctx, t, c, nil); err != nil {
			return s.wait()
		}
		return nil
	}
	if r.Receipt != nil && recoveryNeedsIntervention(r.Receipt.NextAction) {
		return s.parkRecoveryOutput(ctx, t, c, s.recoveryMessageBlocker(ctx, c, i, r.Receipt.Reason, r.Receipt.NextAction))
	}
	out := s.result(ctx, i, structuredText(s.languageFor(ctx, i) == "ru",
		"Your message is saved. Send another message to retry delivery.",
		"Сообщение сохранено. Отправь новое сообщение, чтобы повторить доставку."),
		fmt.Sprintf("recovery-wait:%d", r.WakeSequence))
	return s.parkRecoveryOutput(ctx, t, c, out)
}

func (s *Service) acceptRecoveryReceipt(ctx context.Context, t *db.Task, c checkpoint, i intent, user db.User,
	service string, auth loom.Auth, receipt loom.RecoveryReceipt) error {
	r := c.Recovery
	r.Receipt = &receipt
	if err := s.saveRecovery(ctx, t, c, nil); err != nil {
		return s.wait()
	}
	if receipt.Abandoned {
		return s.finish(ctx, t, c, i, stateCancelled, stateCancelled)
	}
	if !receipt.Delivered {
		return s.pendingRecoveryReceipt(ctx, t, c, i, user, service, auth, receipt)
	}
	return s.attachRecoveredRun(ctx, t, c, i, auth, receipt)
}

func (s *Service) pendingRecoveryReceipt(ctx context.Context, t *db.Task, c checkpoint, i intent, user db.User,
	service string, auth loom.Auth, receipt loom.RecoveryReceipt) error {
	// A pending receipt does not establish observation. Shutdown resumes only
	// after a delivered receipt has attached through OwnRecoveredRun.
	if receipt.NextAction == actionReauthorize {
		if err := s.reauthorizeRecovery(ctx, t, &c, i, user, service, auth); err != nil {
			return err
		}
		if terminal(t.State) || t.State == stateAuthUnknown {
			return nil
		}
		// The renewal receipt is durable. Replay the message on the next attempt
		// before claiming authorization is still blocked.
		if c.Run == nil && c.Session.ID != "" {
			return s.wait()
		}
	}
	out := s.recoveryMessageBlocker(ctx, c, i, receipt.Reason, receipt.NextAction)
	if recoveryNeedsIntervention(receipt.NextAction) {
		return s.parkRecoveryOutput(ctx, t, c, out)
	}
	return s.waitWithRecoveryOutput(ctx, t, c, out)
}

// Report a retained message's blocker once per customer retry window.
func (s *Service) recoveryMessageBlocker(
	ctx context.Context, c checkpoint, i intent, reason, action string,
) []db.Outbound {
	out := s.recoveryBlocker(ctx, i, reason, action)
	for n := range out {
		out[n].DedupKey += fmt.Sprintf(":wake:%d", c.Recovery.WakeSequence)
	}
	return out
}

func recoveryNeedsIntervention(action string) bool {
	switch action {
	case actionFundWallet, actionReconcilePredecessor, "contact_original_actor", actionSelectModel,
		actionStopThenCreate, actionStopThenSend, "abandon_message_or_inspect_run", actionResolveThenResume:
		return true
	default:
		return false
	}
}

func (s *Service) waitWithRecoveryOutput(ctx context.Context, t *db.Task, c checkpoint, out []db.Outbound) error {
	return s.recoveryOutput(ctx, t, c, out, s.recoveryRetryDelay(c))
}

// Derive backoff from the durable retry window, so restart does not reset it.
func (s *Service) recoveryRetryDelay(c checkpoint) time.Duration {
	delay := s.options.PollInterval
	if c.Recovery == nil || c.Recovery.Receipt != nil && c.Recovery.Receipt.Delivered {
		return delay
	}
	elapsed := recoveryRetryWindow - time.Until(c.Recovery.RetryUntil)
	for elapsed >= recoveryBackoffStep && delay < recoveryBackoffLimit {
		delay = min(delay*recoveryBackoffFactor, recoveryBackoffLimit)
		elapsed -= recoveryBackoffStep
	}
	return delay
}

func (s *Service) parkRecoveryOutput(ctx context.Context, t *db.Task, c checkpoint, out []db.Outbound) error {
	return s.recoveryOutput(ctx, t, c, out, time.Minute)
}

func (s *Service) recoveryOutput(ctx context.Context, t *db.Task, c checkpoint, out []db.Outbound,
	delay time.Duration) error {
	if err := s.saveRecovery(ctx, t, c, out); err != nil {
		return s.wait()
	}
	if err := s.deliver(ctx, t, c); err != nil {
		return err
	}
	return river.JobSnooze(delay)
}

func (s *Service) saveRecovery(ctx context.Context, t *db.Task, c checkpoint, out []db.Outbound) error {
	raw, err := json.Marshal(c)
	if err != nil {
		return err
	}
	keys := []string{}
	for _, item := range s.appendStartClosing(ctx, *t, out) {
		keys = append(keys, t.ID.String()+":"+item.DedupKey)
	}
	unchanged, err := s.store.RecoveryOutputUnchanged(ctx, *t, s.owner, raw, keys)
	if err != nil {
		s.reportDatabaseFailure(ctx, t.ID, "recovery_checkpoint_read", err)
		return err
	}
	if unchanged {
		return nil
	}
	return s.save(ctx, t, c, out)
}

func recoveryPauseAction(run loom.Run) string {
	if run.Status != runPaused {
		return ""
	}
	switch run.Reason {
	case "credits_exhausted":
		return actionFundWallet
	case "recovery_context_unavailable", "recovery_context_too_large":
		return actionStopThenSend
	default:
		return ""
	}
}

func (s *Service) recoveryRunBlocker(ctx context.Context, i intent, run loom.Run, action string) []db.Outbound {
	out := s.recoveryBlocker(ctx, i, run.Reason, action)
	for n := range out {
		out[n].DedupKey += fmt.Sprintf(":run:%s:%d", run.ID, run.Version)
	}
	return out
}

func (s *Service) attachRecoveredRun(ctx context.Context, t *db.Task, c checkpoint, i intent,
	auth loom.Auth, receipt loom.RecoveryReceipt) error {
	run, err := s.readConversationRun(ctx, &c, auth, receipt.RunID)
	if err != nil {
		return s.loomError(ctx, t, c, i, err)
	}
	if err = s.confirmRecoveryPredecessor(ctx, t, &c, i, auth, run); err != nil {
		return err
	}
	if err = s.restoreRecoverySession(ctx, *t, &c, run); err != nil {
		s.reportDatabaseFailure(ctx, t.ID, "recovery_session_restore", err)
		return s.wait()
	}
	t.RunID, t.AuthSessionID = run.ID, run.AuthSessionID
	c.Run = &run
	snapshot, err := json.Marshal(c)
	if err != nil {
		return err
	}
	owned, observer, err := s.store.OwnRecoveredRun(ctx, *t, s.owner, snapshot)
	if err != nil {
		return s.recoveryAttachmentError(ctx, t, c, i, err)
	}
	*t = owned
	if !observer {
		t.State = stateCompleted
		out := s.stepLimitInputAck(ctx, i, loom.ControlReceipt{Status: run.Status, Reason: run.Reason})
		if receipt.NextAction != actionResolveThenResume && receipt.NextAction != actionReauthorize {
			out = append(out, s.recoveryBlocker(ctx, i, receipt.Reason, receipt.NextAction)...)
		}
		if err = s.save(ctx, t, c, out); err != nil {
			return s.wait()
		}
		return s.deliverAndContinueStart(ctx, t, c, i)
	}
	if err = json.Unmarshal(owned.Snapshot, &c); err != nil {
		return s.wait()
	}
	if err = s.registerRunReactions(ctx, t, &c, run); err != nil {
		return s.wait()
	}
	return s.observeRun(ctx, t, c, i, run)
}

func (s *Service) recoveryAttachmentError(ctx context.Context, t *db.Task, c checkpoint, i intent, err error) error {
	if errors.Is(err, db.ErrRecoveryAuthorizationUnknown) {
		return s.parkRecoveryOutput(ctx, t, c,
			s.recoveryBlocker(ctx, i, "authorization_outcome_unknown", actionReconcilePredecessor))
	}
	if errors.Is(err, db.ErrRoutingPending) {
		return s.waitForRecoveryOrder(*t)
	}
	s.reportDatabaseFailure(ctx, t.ID, "recovery_attach", err)
	return s.wait()
}

func (s *Service) restoreRecoverySession(ctx context.Context, t db.Task, c *checkpoint, run loom.Run) error {
	prior, err := s.store.RecoveryRunSession(ctx, t.UserID, run.ID, run.AuthSessionID)
	if err != nil || len(prior) == 0 {
		return err
	}
	var saved checkpoint
	if err = json.Unmarshal(prior, &saved); err != nil {
		return err
	}
	if c.Session.ID != run.AuthSessionID || saved.Session.Version > c.Session.Version {
		c.Session = saved.Session
		c.SessionVersionUnknown = saved.Reauthorizing || saved.SessionVersionUnknown
	}
	if saved.Session.Version == c.Session.Version && (saved.Reauthorizing || saved.SessionVersionUnknown) {
		c.SessionVersionUnknown = true
	}
	if saved.Run != nil && (saved.Run.ID == run.ID || saved.Run.ChatID != "" && saved.Run.ChatID == run.ChatID) {
		c.KeyboardPrompt, c.KeyboardRevision = saved.KeyboardPrompt, saved.KeyboardRevision
	}
	return nil
}

func (s *Service) confirmRecoveryPredecessor(ctx context.Context, t *db.Task, c *checkpoint,
	i intent, auth loom.Auth, run loom.Run) error {
	if run.ReplacesRunID == "" ||
		c.Recovery.SupersededRunID == run.ReplacesRunID {
		return nil
	}
	previous, err := s.loom.GetRun(ctx, auth, run.ReplacesRunID)
	if err != nil {
		return s.recoveryCapabilityError(ctx, t, *c, i, err)
	}
	c.DiscoveryRetryUntil, c.DiscoveryNextCheck = nil, time.Time{}
	if previous.Status == stateCancelled && previous.Reason == "superseded" {
		c.Recovery.SupersededRunID = previous.ID
	}
	return nil
}

func (s *Service) reauthorizeRecovery(
	ctx context.Context,
	t *db.Task,
	c *checkpoint,
	i intent,
	user db.User,
	service string,
	auth loom.Auth,
) error {
	located, err := s.loom.ActiveRun(ctx, auth, c.Recovery.ChatID)
	if err != nil {
		return s.loomError(ctx, t, *c, i, err)
	}
	if located.RunID == "" {
		return nil
	}
	run, err := s.loom.GetRun(ctx, auth, located.RunID)
	if err != nil {
		return s.loomError(ctx, t, *c, i, err)
	}
	observer, err := s.store.RecoveryObserver(ctx, *t)
	if err != nil {
		s.reportDatabaseFailure(ctx, t.ID, "recovery_renewal_observer", err)
		return s.wait()
	}
	if observer.ID != t.ID && observer.RunID == run.ID && observer.AuthSessionID == run.AuthSessionID &&
		!terminal(observer.State) {
		// The current observer owns renewal. Parallel CAS renewals would leave
		// one task with an older receipt even after authorization succeeded.
		return s.wait()
	}
	raw, err := s.store.RecoveryRunSession(ctx, t.UserID, run.ID, run.AuthSessionID)
	if err != nil {
		s.reportDatabaseFailure(ctx, t.ID, "recovery_session_lookup", err)
		return s.wait()
	}
	var prior checkpoint
	if len(raw) == 0 {
		return nil
	}
	if err = json.Unmarshal(raw, &prior); err != nil {
		return s.wait()
	}
	if prior.Session.ID != run.AuthSessionID {
		return nil
	}
	c.Session = prior.Session
	t.AuthSessionID = run.AuthSessionID
	if prior.Reauthorizing || prior.SessionVersionUnknown {
		c.SessionVersionUnknown = true
		return s.parkUnknownSession(ctx, t, *c, i)
	}
	// Force the existing reauthorization flow, retaining its unknown-outcome guard.
	c.Run = &loom.Run{Status: runWaitingAuth, Reason: reasonAuthorizationRequired}
	_, err = s.ensureSession(ctx, t, c, i, user, service)
	return err
}

func (s *Service) observeRecoveredRun(
	ctx context.Context,
	t *db.Task,
	c checkpoint,
	i intent,
	user db.User,
	service string,
) error {
	if ready, err := s.prepareObservationSession(ctx, t, &c, i, user, service); !ready {
		return err
	}
	credential, err := s.iam.UserToken(ctx, user.Subject)
	if err != nil {
		return s.identityError(ctx, t, c, i, err)
	}
	auth := loom.Auth{ServiceToken: service, SubjectToken: credential.Token}
	run, err := s.readConversationRun(ctx, &c, auth, t.RunID)
	if err != nil {
		return s.loomError(ctx, t, c, i, err)
	}
	if run.Status == runWaitingAuth {
		c.Run = &run
		if c.SessionVersionUnknown {
			return s.parkUnknownSession(ctx, t, c, i)
		}
		if run.Reason == "permission_revoked" {
			return s.finish(ctx, t, c, i, stateFailed, "denied")
		}
		if runNeedsAuthorization(run) {
			return s.waitForLoomAuthorization(ctx, t, c, i)
		}
		t.State = stateAwaitingAuth
		return s.waitWithRecoveryOutput(ctx, t, c,
			s.recoveryBlocker(ctx, i, run.Reason, actionResolveThenRetry))
	}
	if run.Status == runPaused && run.Reason == reasonShutdown {
		if c.SessionVersionUnknown {
			return s.parkUnknownSession(ctx, t, c, i)
		}
		return s.resumeShutdown(ctx, t, c, i, auth, run)
	}
	c.ResumeKey = ""
	c.LoomAuthRetryUntil = nil
	if err = s.registerRunReactions(ctx, t, &c, run); err != nil {
		return s.wait()
	}
	return s.observeRun(ctx, t, c, i, run)
}

func (s *Service) recoveryBlocker(ctx context.Context, i intent, reason, action string) []db.Outbound {
	if reason == "" {
		return nil
	}
	if reason == reasonRecoveryAuthorizationRequired {
		action = actionReauthorize
	}
	var en, ru string
	switch action {
	case "ask_user_to_continue":
		// The run observer owns the progress note and Continue/Stop prompt.
		return nil
	case actionFundWallet:
		en = "Your budget is used. Add funds or wait for the next funding, then send a message to retry."
		ru = "Бюджет исчерпан. Пополни баланс или дождись следующего пополнения, " +
			"затем отправь сообщение для повтора."
	case actionReauthorize:
		en = "Your message is saved. Authorization must be renewed before work can continue."
		ru = "Сообщение сохранено. Для продолжения нужно обновить авторизацию."
	case actionSelectModel:
		en = "Your message is saved. Select an available model or restore access to the current model."
		ru = "Сообщение сохранено. Выбери доступную модель или восстанови доступ к текущей."
	case actionReconcilePredecessor:
		en = "Your message is saved. An administrator must check the previous action's outcome before work continues."
		ru = "Сообщение сохранено. Администратор должен проверить результат предыдущего действия " +
			"перед продолжением."
	case "retry_same_message":
		en = "Your message is saved. Waiting for the previous task to stop before continuing."
		ru = "Сообщение сохранено. Жду остановки предыдущей задачи перед продолжением."
	case "wait_for_active_run":
		en = "Your request is saved. Another run is still active. " +
			"Wait for it to finish or ask an administrator to reconcile it."
		ru = "Запрос сохранён. Другая задача ещё выполняется. Дождись её завершения или попроси администратора проверить её."
	case "retry_earlier_message":
		en = "Your message is saved and is waiting for an earlier message to be processed."
		ru = "Сообщение сохранено и ожидает обработки более раннего сообщения."
	case "contact_original_actor":
		en = "Your message is saved. The client that sent the earlier message must resolve or withdraw it first."
		ru = "Сообщение сохранено. Сначала нужно обработать или отозвать предыдущее сообщение " +
			"в приложении, которое его отправило."
	case "abandon_message_or_inspect_run":
		en = "Your message is saved. The run changed; ask an administrator to inspect it or withdraw the undelivered message."
		ru = "Сообщение сохранено. Задача изменилась: попроси администратора проверить её " +
			"или отозвать недоставленное сообщение."
	case actionStopThenCreate, actionStopThenSend:
		en = "Your message is saved. Ask an administrator to stop the previous run safely, then retry in a new round."
		ru = "Сообщение сохранено. Попроси администратора безопасно остановить прежнюю задачу, " +
			"затем повтори запрос в новом запуске."
	case "restore_service_then_retry":
		en = "Your message is retained. The agent service is unavailable; " +
			"delivery and result checks will retry automatically."
		ru = "Сообщение сохранено. Сервис агента недоступен; доставка и проверка результата повторятся автоматически."
	case actionResolveThenRetry:
		en = "Your message is saved. Check the service limit or access restriction, then retry. " +
			"If agent capacity is full, wait for work to finish."
		ru = "Сообщение сохранено. Проверь лимит сервиса или ограничение доступа, затем повтори попытку. " +
			"Если все агенты заняты, дождись завершения работы."
	case actionRestoreChatAccess, "restore_agent_access", actionRestoreRecoveryAccess:
		en, ru = recoveryAccessText(action)
	default:
		en = "Your message is saved. Work is blocked; contact an administrator to resolve it."
		ru = "Сообщение сохранено. Работа заблокирована; обратись к администратору для устранения причины."
	}
	return s.result(ctx, i, structuredText(s.languageFor(ctx, i) == "ru", en, ru), "recovery-blocked:"+reason+":"+action)
}

func recoveryAccessText(action string) (string, string) {
	if action == actionRestoreRecoveryAccess {
		return "Your request is saved. Agent recovery access is unavailable. " +
				"Ask an administrator to restore permissions; retries will continue automatically.",
			"Запрос сохранён. Нет доступа к восстановлению работы агента. " +
				"Попроси администратора восстановить права; повторные проверки продолжатся автоматически."
	}
	if action == "restore_agent_access" {
		return "Your request is saved. Agent creation is not permitted. " +
				"Ask an administrator to restore agent permissions or model access; retries will continue automatically.",
			"Запрос сохранён. Создание агента запрещено. Попроси администратора восстановить права агента " +
				"или доступ к модели; повторные проверки продолжатся автоматически."
	}
	return "Your message is saved. Access to the original conversation is unavailable. " +
			"Restore access or ask an administrator to inspect it; retries will continue automatically.",
		"Сообщение сохранено. Нет доступа к исходному разговору. " +
			"Восстанови доступ или попроси администратора проверить его; повторные проверки продолжатся автоматически."
}
