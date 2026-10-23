package task

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/riverqueue/river"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/files"
	"github.com/complynx/zns-chatbot/internal/identity"
	"github.com/complynx/zns-chatbot/internal/loom"
)

var errControlTargetFinished = errors.New("control target finished without applying input")

type checkpoint struct {
	NativeAvatar          *db.AvatarDraftReceipt `json:"native_avatar,omitempty"`
	KeyboardRevision      int64                  `json:"keyboard_revision,omitempty"`
	InputReceipt          *loom.ControlReceipt   `json:"input_receipt,omitempty"`
	KeyboardPrompt        string                 `json:"keyboard_prompt,omitempty"`
	RegistrationEntry     *db.RegistrationEntry  `json:"registration_entry,omitempty"`
	RegistrationChange    *db.RegistrationChange `json:"registration_change,omitempty"`
	RegistrationActor     string                 `json:"registration_actor,omitempty"`
	RegistrationOperation string                 `json:"registration_operation,omitempty"`
	ProfileChange         *db.ProfileMutation    `json:"profile_change,omitempty"`
	ResumeKey             string                 `json:"resume_key,omitempty"`
	LoomAuthRetryUntil    *time.Time             `json:"loom_auth_retry_until,omitempty"`
	Session               loom.Session           `json:"session"`
	BootstrapAttempts     int                    `json:"bootstrap_attempts,omitempty"`
	Reauthorizing         bool                   `json:"reauthorizing,omitempty"`
	Create                *loom.CreateRunRequest `json:"create,omitempty"`
	Run                   *loom.Run              `json:"run,omitempty"`
	Control               *loom.InputRequest     `json:"control,omitempty"`
	ControlUnapplied      bool                   `json:"control_unapplied,omitempty"`
	ReplyModeMismatch     bool                   `json:"reply_mode_mismatch,omitempty"`
}

func (s *Service) save(ctx context.Context, t *db.Task, c checkpoint, out []db.Outbound) error {
	// Outbox keys are global, and all messages of a task share one delivery order.
	if len(out) > 0 {
		existing, err := s.store.ListOutbox(ctx, t.ID)
		if err != nil {
			s.reportDatabaseFailure(ctx, t.ID, "outbox_read", err)
			return err
		}
		keys := make(map[string]bool, len(existing))
		var next int32
		for _, row := range existing {
			keys[row.DedupKey] = true
			if row.ChunkSeq >= next {
				next = row.ChunkSeq + 1
			}
		}
		fresh := make([]db.Outbound, 0, len(out))
		for _, item := range out {
			item.DedupKey = t.ID.String() + ":" + item.DedupKey
			if keys[item.DedupKey] {
				continue
			}
			item.ID = uuid.NewSHA1(uuid.NameSpaceOID, []byte(item.DedupKey))
			item.ChunkSeq = next
			next++
			fresh = append(fresh, item)
		}
		out = fresh
	}
	b, err := json.Marshal(c)
	if err != nil {
		return err
	}
	t.Snapshot = b
	saved, err := s.store.Save(ctx, *t, s.owner, t.Fence, out)
	if err == nil {
		*t = saved
	} else {
		s.reportDatabaseFailure(ctx, t.ID, "checkpoint_save", err)
	}
	return err
}
func terminal(state string) bool {
	return state == stateCompleted || state == stateFailed || state == stateCancelled ||
		state == stateDeliveryUnknown ||
		state == stateDeliveryFailed || state == stateClosed
}
func finishedRun(status string) bool {
	return terminal(status) || status == "succeeded" || status == "canceled"
}
func (s *Service) wait() error { return river.JobSnooze(s.options.PollInterval) }
func (s *Service) work(parent context.Context, id uuid.UUID) error {
	ctx, cancel := context.WithTimeout(parent, s.options.AttemptTimeout)
	defer cancel()
	t, err := s.store.Route(ctx, id)
	if err != nil {
		s.reportDatabaseFailure(ctx, id, "route", err)
		return s.wait()
	}
	t, err = s.store.Claim(ctx, id, s.owner, s.options.LeaseDuration)
	if err != nil {
		s.reportDatabaseFailure(ctx, id, "claim", err)
		return s.wait()
	}
	defer s.releaseTask(ctx, &t)
	if t.State == stateClosed {
		return nil
	}
	var i intent
	var c checkpoint
	if json.Unmarshal(t.Request, &i) != nil || json.Unmarshal(t.Snapshot, &c) != nil {
		return errors.New("invalid task checkpoint")
	}
	ctx, end := s.traceAttempt(ctx, i)
	defer end()
	if terminal(t.State) || t.State == stateAuthUnknown {
		return s.deliver(ctx, &t, c)
	}
	if c.Reauthorizing {
		return s.finish(ctx, &t, c, i, stateAuthUnknown, stateAuthUnknown)
	}
	if i.Kind == kindUnsupported {
		return s.finish(ctx, &t, c, i, stateFailed, kindUnsupported)
	}
	if t.Route == "no_active" {
		return s.finish(ctx, &t, c, i, stateCompleted, "no_active")
	}
	if t.Route == kindStructured {
		if i.NativeAvatar {
			return s.nativeAvatarPhoto(ctx, &t, c, i)
		}
		return s.structured(ctx, &t, c, i)
	}
	if t.TargetTaskID != uuid.Nil {
		ready, gateErr := s.store.ControlReady(ctx, t.ID)
		if gateErr != nil {
			s.reportDatabaseFailure(ctx, t.ID, "control_gate", gateErr)
			return s.wait()
		}
		if !ready {
			return s.wait()
		}
	}
	user, service, err := s.authorize(ctx, t, i)
	if err != nil {
		return s.identityError(ctx, &t, c, i, err)
	}
	return s.authorizedTask(ctx, &t, c, i, user, service.Token)
}

func (s *Service) authorizedTask(
	ctx context.Context, t *db.Task, c checkpoint, i intent, user db.User, service string,
) error {
	ready, startErr := s.prepareStart(ctx, t, c, i)
	if !ready {
		return startErr
	}
	if t.Route != "new_run" {
		return s.control(ctx, t, c, i, user, service)
	}
	return s.runTask(ctx, t, c, i, user, service)
}
func (s *Service) runTask(ctx context.Context, t *db.Task, c checkpoint, i intent, user db.User, service string) error {
	ready, err := s.ensureSession(ctx, t, &c, i, user, service)
	if !ready {
		return err
	}
	return s.advanceRun(ctx, t, c, i, loom.Auth{ServiceToken: service, SessionID: t.AuthSessionID})
}
func (s *Service) releaseTask(parent context.Context, t *db.Task) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), time.Second)
	defer cancel()
	if err := s.store.Release(ctx, t.ID, s.owner, t.Fence); err != nil {
		s.reportDatabaseFailure(ctx, t.ID, "lease_release", err)
	}
}
func (s *Service) traceAttempt(ctx context.Context, i intent) (context.Context, func()) {
	if s.observer == nil {
		return ctx, func() {}
	}
	accepted := s.observer.Propagator().
		Extract(ctx, propagation.MapCarrier{"traceparent": i.TraceParent, "tracestate": i.TraceState})
	var options []trace.SpanStartOption
	if linked := trace.SpanContextFromContext(accepted); linked.IsValid() {
		options = append(options, trace.WithLinks(trace.Link{SpanContext: linked}))
	}
	ctx, span := s.observer.Tracer().Start(ctx, "task.process", options...)
	return ctx, func() { span.End() }
}
func (s *Service) authorize(ctx context.Context, t db.Task, i intent) (db.User, identity.Credential, error) {
	user, err := s.store.GetUser(ctx, t.UserID)
	if err != nil {
		s.reportDatabaseFailure(ctx, t.ID, "user_read", err)
		return user, identity.Credential{}, err
	}
	if user.Revoked {
		s.iam.Invalidate(user.Subject)
		return user, identity.Credential{}, identity.ErrDenied
	}
	if user.Subject == "" {
		bot, organization, _ := s.iam.DeploymentScope()
		account, ensureErr := s.iam.EnsureUser(
			ctx,
			i.User,
			t.UserID.String(),
			func(ctx context.Context, technical bool) error {
				return s.store.RecordIAMNameOrigin(ctx, t.UserID, bot, organization, technical)
			},
		)
		if ensureErr != nil {
			return user, identity.Credential{}, ensureErr
		}
		user, err = s.store.BindSubject(ctx, t.UserID, account.Subject, account.OrganizationID)
		if err != nil {
			s.reportDatabaseFailure(ctx, t.ID, "subject_bind", err)
			return user, identity.Credential{}, err
		}
	}
	service, err := s.iam.ServiceToken(ctx)
	if err != nil {
		return user, service, err
	}
	latest, err := s.store.GetUser(ctx, t.UserID)
	if err != nil {
		s.reportDatabaseFailure(ctx, t.ID, "user_recheck", err)
		return user, service, err
	}
	if latest.Revoked {
		s.iam.Invalidate(user.Subject)
		return user, service, identity.ErrDenied
	}
	return user, service, nil
}

func (s *Service) ensureSession(
	ctx context.Context,
	t *db.Task,
	c *checkpoint,
	i intent,
	user db.User,
	service string,
) (bool, error) {
	recovering := c.Run != nil && c.Run.Status == "waiting_auth" && c.Run.Reason == "authorization_required"
	if t.AuthSessionID != "" && c.Session.ExpiresAt.After(time.Now().Add(sessionRenewSkew)) && !recovering {
		return true, nil
	}
	if recovering {
		if !mayHaveRun(*t, *c) && !c.ReplyModeMismatch && c.LoomAuthRetryUntil != nil &&
			!time.Now().Before(*c.LoomAuthRetryUntil) {
			return false, s.finish(ctx, t, *c, i, stateFailed, "denied")
		}
		s.iam.Invalidate(user.Subject)
	}
	if t.AuthSessionID == "" && c.BootstrapAttempts >= maxBootstrapAttempts {
		return false, s.finish(ctx, t, *c, i, stateAuthUnknown, stateAuthUnknown)
	}
	credential, err := s.iam.UserToken(ctx, user.Subject)
	if err != nil {
		return false, s.identityError(ctx, t, *c, i, err)
	}
	// A valid cached fallback may not extend the session. Avoid an unnecessary mutation
	// until a fresher credential is available or the existing authorization expires.
	if t.AuthSessionID != "" && !recovering && c.Session.ExpiresAt.After(time.Now()) &&
		!credential.ExpiresAt.After(c.Session.ExpiresAt) {
		return true, nil
	}
	auth := loom.Auth{ServiceToken: service, SubjectToken: credential.Token}
	session, err := s.renewSession(ctx, t, c, auth)
	if err != nil {
		return false, s.sessionError(ctx, t, c, i, err)
	}
	c.Session = session
	c.Reauthorizing = false
	if recovering {
		// The receipt is durable before a fresh run read; do not renew twice after restart.
		c.Run = nil
	}
	t.AuthSessionID = session.ID
	t.State = "running"
	// Persist this receipt before creating a run.
	if err = s.save(ctx, t, *c, nil); err != nil {
		return false, s.wait()
	}
	return true, nil
}
func (s *Service) renewSession(ctx context.Context, t *db.Task, c *checkpoint, auth loom.Auth) (loom.Session, error) {
	if t.AuthSessionID == "" {
		c.BootstrapAttempts++
		t.State = stateAwaitingAuth
	} else {
		// Reauthorize has no idempotency contract; persist uncertainty before I/O.
		c.Reauthorizing = true
		t.State = "auth_reauthorizing"
	}
	if err := s.save(ctx, t, *c, nil); err != nil {
		return loom.Session{}, err
	}
	if t.AuthSessionID == "" {
		return s.loom.Bootstrap(ctx, auth)
	}
	return s.loom.Reauthorize(ctx, auth, t.AuthSessionID, c.Session.Version)
}
func (s *Service) sessionError(ctx context.Context, t *db.Task, c *checkpoint, i intent, err error) error {
	var upstream *loom.Error
	known := errors.As(err, &upstream)
	if known && upstream.Outcome == loom.Unknown && c.Reauthorizing {
		return s.finish(ctx, t, *c, i, stateAuthUnknown, stateAuthUnknown)
	}
	c.Reauthorizing = false
	if t.AuthSessionID == "" && known && upstream.Outcome != loom.Unknown {
		c.BootstrapAttempts--
	}
	if known && upstream.StatusCode == http.StatusUnauthorized && upstream.Outcome == loom.Rejected {
		return s.waitForLoomAuthorization(ctx, t, *c, i)
	}
	if known && !upstream.Retryable && upstream.Outcome == loom.Rejected {
		if c.ReplyModeMismatch {
			return s.waitForLoomAuthorization(ctx, t, *c, i)
		}
		return s.finish(ctx, t, *c, i, stateFailed, "denied")
	}
	c.LoomAuthRetryUntil = nil
	t.State = stateAwaitingAuth
	_ = s.save(ctx, t, *c, nil)
	return s.wait()
}
func (s *Service) advanceRun(ctx context.Context, t *db.Task, c checkpoint, i intent, auth loom.Auth) error {
	if c.ReplyModeMismatch {
		return s.reconcileIncompatibleRun(ctx, t, c, i, auth)
	}
	var run loom.Run
	var err error
	if t.RunID == "" {
		if err = s.prepareCreate(ctx, t, &c, i); err != nil {
			return s.wait()
		}
		run, err = s.loom.CreateRun(ctx, auth, t.ID.String(), *c.Create)
	} else {
		run, err = s.loom.GetRun(ctx, auth, t.RunID)
	}
	if err != nil {
		return s.runRequestError(ctx, t, c, i, run, err)
	}
	if run.AuthSessionID != "" && run.AuthSessionID != t.AuthSessionID {
		return s.finish(ctx, t, c, i, stateFailed, "denied")
	}
	if run.Status == "paused" && run.Reason == "shutdown" {
		return s.resumeShutdown(ctx, t, c, i, auth, run)
	}
	if run.Status == "waiting_auth" {
		t.RunID = run.ID
		c.Run = &run
		if run.Reason == "permission_revoked" {
			return s.finish(ctx, t, c, i, stateFailed, "denied")
		}
		if run.Reason == "authorization_required" {
			return s.waitForLoomAuthorization(ctx, t, c, i)
		}
		t.State = stateAwaitingAuth
		if err = s.save(ctx, t, c, nil); err != nil {
			return s.wait()
		}
		return s.wait()
	}
	c.ResumeKey = ""
	c.LoomAuthRetryUntil = nil
	return s.observeRun(ctx, t, c, i, run)
}
func (s *Service) observeRun(ctx context.Context, t *db.Task, c checkpoint, i intent, run loom.Run) error {
	t.RunID = run.ID
	c.Run = &run
	t.QuestionID = ""
	switch run.Status {
	case stateCompleted, "succeeded":
		t.State = stateCompleted
		out := s.result(i, run.Result, "result")
		if c.KeyboardPrompt != "" {
			if len(out) == 0 {
				out = s.result(i, structuredText(s.languageFor(ctx, i) == "ru", "Done.", "Готово."), "result")
			}
			removeReplyKeyboard(out)
			c.KeyboardPrompt = ""
		}
		if err := s.save(ctx, t, c, out); err != nil {
			return s.wait()
		}
		return s.deliver(ctx, t, c)
	case stateFailed:
		return s.finish(ctx, t, c, i, stateFailed, stateFailed)
	case stateCancelled, "canceled":
		return s.finish(ctx, t, c, i, stateCancelled, stateCancelled)
	default:
		t.State = "running"
		if run.Status == "paused" || run.Status == "reconciling" {
			t.State = stateAwaitingService
		}
		out, err := s.pendingRunOutput(ctx, t, &c, i, run)
		if err != nil {
			s.reportDatabaseFailure(ctx, t.ID, "approval_presentation", err)
			return s.wait()
		}
		if err = s.save(ctx, t, c, out); err != nil {
			return s.wait()
		}
		if err = s.deliver(ctx, t, c); err != nil {
			return err
		}
		if terminal(t.State) {
			return nil
		}
		return s.wait()
	}
}
func (s *Service) identityError(ctx context.Context, t *db.Task, c checkpoint, i intent, err error) error {
	if errors.Is(err, identity.ErrDenied) || errors.Is(err, identity.ErrConflict) {
		if c.ReplyModeMismatch {
			return s.waitForLoomAuthorization(ctx, t, c, i)
		}
		return s.finish(ctx, t, c, i, stateFailed, "denied")
	}
	c.LoomAuthRetryUntil = nil
	t.State = stateAwaitingAuth
	_ = s.save(ctx, t, c, nil)
	return s.wait()
}
func (s *Service) loomError(ctx context.Context, t *db.Task, c checkpoint, i intent, err error) error {
	var upstream *loom.Error
	if errors.As(err, &upstream) && upstream.Outcome == loom.Rejected &&
		upstream.StatusCode == http.StatusUnauthorized {
		return s.waitForLoomAuthorization(ctx, t, c, i)
	}
	if upstream != nil && upstream.Outcome == loom.Rejected && !upstream.Retryable {
		return s.finish(ctx, t, c, i, stateFailed, stateFailed)
	}
	c.LoomAuthRetryUntil = nil
	t.State = stateAwaitingService
	_ = s.save(ctx, t, c, nil)
	return s.wait()
}
func (s *Service) control(ctx context.Context, t *db.Task, c checkpoint, i intent, user db.User, service string) error {
	target, err := s.store.GetTask(ctx, t.TargetTaskID)
	if err != nil {
		s.reportDatabaseFailure(ctx, t.ID, "control_target", err)
		return s.wait()
	}
	if target.UserID != t.UserID || target.ConversationID != t.ConversationID {
		return s.finish(ctx, t, c, i, stateFailed, "denied")
	}
	if terminal(target.State) || target.State == stateAuthUnknown {
		return s.finishedTarget(ctx, t, c, i, user, service, target)
	}
	if target.RunID == "" {
		return s.wait()
	}
	var tc checkpoint
	if json.Unmarshal(target.Snapshot, &tc) != nil {
		return s.finish(ctx, t, c, i, stateFailed, stateFailed)
	}
	if tc.ReplyModeMismatch {
		return s.wait()
	}
	if tc.Reauthorizing || !tc.Session.ExpiresAt.After(time.Now().Add(controlSessionSkew)) {
		return s.wait()
	}
	auth := loom.Auth{ServiceToken: service, SessionID: target.AuthSessionID}
	run, err := s.loom.GetRun(ctx, auth, target.RunID)
	if err != nil {
		return s.loomError(ctx, t, c, i, err)
	}
	if run.AuthSessionID != "" && run.AuthSessionID != target.AuthSessionID {
		return s.finish(ctx, t, c, i, stateFailed, "denied")
	}
	if finishedRun(run.Status) {
		return s.finishedTarget(ctx, t, c, i, user, service, target)
	}
	if i.CallbackTaskID != uuid.Nil && i.CallbackTaskID != target.ID {
		return s.finish(ctx, t, c, i, stateFailed, "stale")
	}
	applied, err := s.sendControl(ctx, t, &c, i, auth, target.RunID, run)
	if !applied {
		if errors.Is(err, errControlTargetFinished) {
			return s.finishedTarget(ctx, t, c, i, user, service, target)
		}
		return err
	}
	t.RunID = target.RunID
	t.AuthSessionID = target.AuthSessionID
	t.State = stateCompleted
	c.Run = &run
	if err = s.save(ctx, t, c, nil); err != nil {
		return s.wait()
	}
	return s.deliver(ctx, t, c)
}

func (s *Service) finishedTarget(
	ctx context.Context,
	t *db.Task,
	c checkpoint,
	i intent,
	user db.User,
	service string,
	target db.Task,
) error {
	if c.Control != nil && !c.ControlUnapplied {
		handled, err := s.reconcileFinishedInput(ctx, t, c, i, user, service, target)
		if handled {
			return err
		}
	}
	if i.Kind != kindMessage {
		return s.finish(ctx, t, c, i, stateCompleted, "no_active")
	}
	promoted, err := s.store.PromoteMessage(ctx, t.ID, s.owner, t.Fence)
	if err != nil {
		s.reportDatabaseFailure(ctx, t.ID, "message_promote", err)
		return s.wait()
	}
	// Promotion retains the immutable accepted task ID and request, with a fresh run checkpoint.
	*t = promoted
	return s.runTask(ctx, t, checkpoint{}, i, user, service)
}

// A prepared Input is uncertain until the same key returns a receipt or run_terminal.
// Fresh delegation also works after the terminal parent's session has expired.
func (s *Service) reconcileFinishedInput(
	ctx context.Context, t *db.Task, c checkpoint, i intent,
	user db.User, service string, target db.Task,
) (bool, error) {
	if target.RunID == "" {
		return true, s.wait()
	}
	credential, err := s.iam.UserToken(ctx, user.Subject)
	if err != nil {
		return true, s.identityError(ctx, t, c, i, err)
	}
	auth := loom.Auth{ServiceToken: service, SubjectToken: credential.Token}
	receipt, err := s.loom.Input(ctx, auth, target.RunID, t.ID.String(), *c.Control)
	if err != nil {
		var upstream *loom.Error
		if errors.As(err, &upstream) && upstream.Outcome == loom.Rejected &&
			upstream.StatusCode == http.StatusConflict && upstream.Code == loom.CodeRunTerminal {
			// Loom checks the retained idempotency record before this terminal rejection.
			// PromoteMessage atomically clears the now-proven-unapplied checkpoint.
			return false, nil
		}
		// A generic rejection cannot disprove an earlier acceptance with a lost receipt.
		t.State = stateAwaitingService
		if saveErr := s.save(ctx, t, c, nil); saveErr != nil {
			return true, s.wait()
		}
		return true, s.wait()
	}
	c.InputReceipt = &receipt
	t.RunID = target.RunID
	t.AuthSessionID = target.AuthSessionID
	t.State = stateCompleted
	if err = s.save(ctx, t, c, nil); err != nil {
		return true, s.wait()
	}
	return true, s.deliver(ctx, t, c)
}

func (s *Service) sendControl(
	ctx context.Context,
	t *db.Task,
	c *checkpoint,
	i intent,
	auth loom.Auth,
	runID string,
	run loom.Run,
) (bool, error) {
	unapplied := c.Control == nil || c.ControlUnapplied
	if err := s.prepareControl(ctx, t, c, i, run); err != nil {
		if errors.Is(err, ErrUnsupportedUpdate) {
			return false, s.finish(ctx, t, *c, i, stateFailed, "stale")
		}
		return false, s.wait()
	}
	if c.ControlUnapplied {
		// Persist uncertainty before another attempt; a crash can hide its receipt.
		c.ControlUnapplied = false
		if err := s.save(ctx, t, *c, nil); err != nil {
			return false, s.wait()
		}
	}
	receipt, err := s.loom.Input(ctx, auth, runID, t.ID.String(), *c.Control)
	if err != nil {
		return false, s.controlError(ctx, t, c, i, auth, runID, unapplied, err)
	}
	c.InputReceipt = &receipt
	return true, nil
}

func (s *Service) controlError(
	ctx context.Context, t *db.Task, c *checkpoint, i intent,
	auth loom.Auth, runID string, unapplied bool, err error,
) error {
	var upstream *loom.Error
	known := errors.As(err, &upstream)
	terminalRejection := known && upstream.Outcome == loom.Rejected &&
		upstream.StatusCode == http.StatusConflict && upstream.Code == loom.CodeRunTerminal
	if terminalRejection || unapplied && known &&
		(upstream.Outcome == loom.NotStarted || upstream.Outcome == loom.Rejected) {
		// A later rejection cannot disprove an earlier unknown effect.
		c.ControlUnapplied = true
	}
	if c.ControlUnapplied && i.Kind == kindMessage && (c.Control.Kind == "steering" || terminalRejection) &&
		upstream != nil && upstream.StatusCode == http.StatusConflict {
		return s.reconcileControlConflict(ctx, t, c, i, auth, runID, err)
	}
	if !c.ControlUnapplied && !unapplied {
		// An earlier unknown effect survives a later unrelated rejection.
		t.State = stateAwaitingService
		_ = s.save(ctx, t, *c, nil)
		return s.wait()
	}
	return s.loomError(ctx, t, *c, i, err)
}

func (s *Service) reconcileControlConflict(
	ctx context.Context, t *db.Task, c *checkpoint, i intent,
	auth loom.Auth, runID string, conflict error,
) error {
	// Completion may race the preceding read. Other conflicts remain errors.
	latest, err := s.loom.GetRun(ctx, auth, runID)
	if err != nil {
		return s.loomError(ctx, t, *c, i, err)
	}
	if latest.AuthSessionID != "" && latest.AuthSessionID != auth.SessionID {
		return s.finish(ctx, t, *c, i, stateFailed, "denied")
	}
	if !finishedRun(latest.Status) {
		return s.loomError(ctx, t, *c, i, conflict)
	}
	// The parent task may not yet have saved its terminal state in PostgreSQL.
	t.State = stateAwaitingService
	if err = s.save(ctx, t, *c, nil); err != nil {
		return s.wait()
	}
	return errControlTargetFinished
}
func controlInput(_ db.Task, i intent, run loom.Run) (loom.InputRequest, error) {
	if i.Kind == kindApproval {
		a := run.Approval
		if a == nil || a.ReplyMode != loom.ReplyModeProtocol || a.Decision != "" ||
			i.PromptKey != approvalPromptKey(*a) ||
			!a.ExpiresAt.After(time.Now()) {
			return loom.InputRequest{}, ErrUnsupportedUpdate
		}
		return loom.InputRequest{Kind: kindApproval, ApprovalID: a.ID, Decision: i.Decision}, nil
	}
	if i.CallbackTaskID != uuid.Nil {
		return loom.InputRequest{}, ErrUnsupportedUpdate
	}
	input := loom.InputRequest{Kind: "steering", Text: files.Prompt(i.Text, i.PreferredLanguage, i.Attachments)}
	if i.ReplyToMessageID > 0 {
		input.Presentation = &loom.InputPresentation{
			ReplyTo: fmt.Sprintf("telegram:%d:%d", i.ChatID, i.ReplyToMessageID),
		}
	}
	return input, nil
}

// A valid caller can disappear from Loom discovery during restart. Keep the
// immutable operation/session while discovery catches up. Expiry-driven renewal
// and its unknown-outcome gate remain unchanged.
func (s *Service) waitForLoomAuthorization(ctx context.Context, t *db.Task, c checkpoint, i intent) error {
	if c.ReplyModeMismatch {
		// A discovery timeout cannot prove an already created run has stopped.
		c.LoomAuthRetryUntil = nil
	} else {
		if c.LoomAuthRetryUntil == nil {
			deadline := time.Now().Add(loomAuthorizationRetryWindow)
			c.LoomAuthRetryUntil = &deadline
		}
		if !time.Now().Before(*c.LoomAuthRetryUntil) && !mayHaveRun(*t, c) {
			return s.finish(ctx, t, c, i, stateFailed, "denied")
		}
	}
	t.State = stateAwaitingAuth
	if err := s.save(ctx, t, c, nil); err != nil {
		return s.wait()
	}
	return s.wait()
}

// A prepared create may have succeeded despite a lost response. Authorization
// failure alone does not prove that its run stopped or release its conversation.
func mayHaveRun(t db.Task, c checkpoint) bool {
	return t.Route == "new_run" && (t.RunID != "" || c.Create != nil)
}

func (s *Service) pendingRunOutput(
	ctx context.Context,
	t *db.Task,
	c *checkpoint,
	i intent,
	run loom.Run,
) ([]db.Outbound, error) {
	t.QuestionID = ""
	out, err := s.pendingApprovalMessages(ctx, t, i, run.Approval)
	if err != nil {
		return nil, err
	}
	if run.Question != nil && run.Question.Answer == nil {
		t.State, t.QuestionID = "awaiting_user", run.Question.ID
		if run.Question.ExpiresAt.After(time.Now()) {
			out = append(out, s.question(i, *run.Question)...)
		}
	}
	nextKeyboard := liveKeyboardID(&run)
	if nextKeyboard != c.KeyboardPrompt {
		c.KeyboardRevision++
	}
	if nextKeyboard != "" {
		// Presentation ownership can return to the same blocked action after a question.
		// Its stable operation identity must not suppress this new keyboard delivery.
		for n := range out {
			if strings.HasPrefix(out[n].DedupKey, nextKeyboard+":") {
				out[n].DedupKey += ":presentation:" + strconv.FormatInt(c.KeyboardRevision, 10)
			}
		}
	}
	if nextKeyboard == "" && c.KeyboardPrompt != "" {
		cleanup := s.result(
			i,
			retiredKeyboardText(run, c.KeyboardPrompt, s.languageFor(ctx, i) == "ru"),
			"keyboard-closed:"+c.KeyboardPrompt+":presentation:"+strconv.FormatInt(c.KeyboardRevision, 10),
		)
		removeReplyKeyboard(cleanup)
		out = append(out, cleanup...)
	}
	c.KeyboardPrompt = nextKeyboard
	return out, nil
}
