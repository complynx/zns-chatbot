package task

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/riverqueue/river"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	"github.com/complynx/shuttle/internal/db"
	"github.com/complynx/shuttle/internal/identity"
	"github.com/complynx/shuttle/internal/loom"
)

type checkpoint struct {
	Session           loom.Session           `json:"session"`
	BootstrapAttempts int                    `json:"bootstrap_attempts,omitempty"`
	Reauthorizing     bool                   `json:"reauthorizing,omitempty"`
	Create            *loom.CreateRunRequest `json:"create,omitempty"`
	Run               *loom.Run              `json:"run,omitempty"`
	Control           *loom.InputRequest     `json:"control,omitempty"`
}

func (s *Service) save(ctx context.Context, t *db.Task, c checkpoint, out []db.Outbound) error {
	// Outbox keys are global, and all messages of a task share one delivery order.
	if len(out) > 0 {
		existing, err := s.store.ListOutbox(ctx, t.ID)
		if err != nil {
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
	}
	return err
}
func terminal(state string) bool {
	return state == stateCompleted || state == stateFailed || state == stateCancelled ||
		state == stateDeliveryUnknown ||
		state == stateDeliveryFailed
}
func (s *Service) wait() error { return river.JobSnooze(s.options.PollInterval) }
func (s *Service) work(parent context.Context, id uuid.UUID) error {
	ctx, cancel := context.WithTimeout(parent, s.options.AttemptTimeout)
	defer cancel()
	t, err := s.store.Route(ctx, id)
	if err != nil {
		return s.wait()
	}
	t, err = s.store.Claim(ctx, id, s.owner, s.options.LeaseDuration)
	if err != nil {
		return s.wait()
	}
	defer s.releaseTask(ctx, &t)
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
	if t.TargetTaskID != uuid.Nil {
		ready, gateErr := s.store.ControlReady(ctx, t.ID)
		if gateErr != nil || !ready {
			return s.wait()
		}
	}
	user, service, err := s.authorize(ctx, t, i)
	if err != nil {
		return s.identityError(ctx, &t, c, i, err)
	}
	if t.Route != "new_run" {
		return s.control(ctx, &t, c, i, user, service.Token)
	}
	return s.runTask(ctx, &t, c, i, user, service.Token)
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
	_ = s.store.Release(ctx, t.ID, s.owner, t.Fence)
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
		return user, identity.Credential{}, err
	}
	if user.Revoked {
		s.iam.Invalidate(user.Subject)
		return user, identity.Credential{}, identity.ErrDenied
	}
	if user.Subject == "" {
		account, ensureErr := s.iam.EnsureUser(ctx, i.User, t.UserID.String())
		if ensureErr != nil {
			return user, identity.Credential{}, ensureErr
		}
		user, err = s.store.BindSubject(ctx, t.UserID, account.Subject, account.OrganizationID)
		if err != nil {
			return user, identity.Credential{}, err
		}
	}
	service, err := s.iam.ServiceToken(ctx)
	if err != nil {
		return user, service, err
	}
	latest, err := s.store.GetUser(ctx, t.UserID)
	if err != nil {
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
	if t.AuthSessionID != "" && c.Session.ExpiresAt.After(time.Now().Add(sessionRenewSkew)) {
		return true, nil
	}
	if t.AuthSessionID == "" && c.BootstrapAttempts >= maxBootstrapAttempts {
		return false, s.finish(ctx, t, *c, i, stateAuthUnknown, stateAuthUnknown)
	}
	credential, err := s.iam.UserToken(ctx, user.Subject)
	if err != nil {
		return false, s.identityError(ctx, t, *c, i, err)
	}
	auth := loom.Auth{ServiceToken: service, SubjectToken: credential.Token}
	session, err := s.renewSession(ctx, t, c, auth)
	if err != nil {
		return false, s.sessionError(ctx, t, c, i, err)
	}
	c.Session = session
	c.Reauthorizing = false
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
	if known && !upstream.Retryable && upstream.Outcome == loom.Rejected {
		return s.finish(ctx, t, *c, i, stateFailed, "denied")
	}
	t.State = stateAwaitingAuth
	_ = s.save(ctx, t, *c, nil)
	return s.wait()
}
func (s *Service) advanceRun(ctx context.Context, t *db.Task, c checkpoint, i intent, auth loom.Auth) error {
	var run loom.Run
	var err error
	if t.RunID == "" {
		if c.Create == nil {
			c.Create = &loom.CreateRunRequest{
				Input:            loom.Input{Text: i.Text},
				Interaction:      loom.Interaction{Questions: "choices_and_text", Approvals: true},
				DisconnectPolicy: "continue",
				Delivery:         loom.Delivery{Mode: "async"},
			}
			t.State = "creating_run"
			if err = s.save(ctx, t, c, nil); err != nil {
				return s.wait()
			}
		}
		run, err = s.loom.CreateRun(ctx, auth, t.ID.String(), *c.Create)
	} else {
		run, err = s.loom.GetRun(ctx, auth, t.RunID)
	}
	if err != nil {
		return s.loomError(ctx, t, c, i, err)
	}
	if run.AuthSessionID != "" && run.AuthSessionID != t.AuthSessionID {
		return s.finish(ctx, t, c, i, stateFailed, "denied")
	}
	return s.observeRun(ctx, t, c, i, run)
}
func (s *Service) observeRun(ctx context.Context, t *db.Task, c checkpoint, i intent, run loom.Run) error {
	t.RunID = run.ID
	c.Run = &run
	t.QuestionID = ""
	switch run.Status {
	case stateCompleted, "succeeded":
		t.State = stateCompleted
		if err := s.save(ctx, t, c, s.result(i, run.Result, "result")); err != nil {
			return s.wait()
		}
		return s.deliver(ctx, t, c)
	case stateFailed:
		return s.finish(ctx, t, c, i, stateFailed, stateFailed)
	case stateCancelled, "canceled":
		return s.finish(ctx, t, c, i, stateCancelled, stateCancelled)
	default:
		t.State = "running"
		var out []db.Outbound
		if run.Question != nil {
			t.State = "awaiting_user"
			t.QuestionID = run.Question.ID
			out = s.question(i, t.ID, *run.Question)
		}
		if run.Approval != nil {
			t.State = "awaiting_user"
			t.QuestionID = run.Approval.ID
			out = s.approval(ctx, i, t.ID, *run.Approval)
		}
		if err := s.save(ctx, t, c, out); err != nil {
			return s.wait()
		}
		if err := s.deliver(ctx, t, c); err != nil {
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
		return s.finish(ctx, t, c, i, stateFailed, "denied")
	}
	t.State = stateAwaitingAuth
	_ = s.save(ctx, t, c, nil)
	return s.wait()
}
func (s *Service) loomError(ctx context.Context, t *db.Task, c checkpoint, i intent, err error) error {
	var upstream *loom.Error
	if errors.As(err, &upstream) && upstream.Outcome == loom.Rejected && !upstream.Retryable {
		return s.finish(ctx, t, c, i, stateFailed, stateFailed)
	}
	t.State = "awaiting_service"
	_ = s.save(ctx, t, c, nil)
	return s.wait()
}
func (s *Service) control(ctx context.Context, t *db.Task, c checkpoint, i intent, user db.User, service string) error {
	target, err := s.store.GetTask(ctx, t.TargetTaskID)
	if err != nil {
		return s.wait()
	}
	if target.UserID != t.UserID || target.ConversationID != t.ConversationID {
		return s.finish(ctx, t, c, i, stateFailed, "denied")
	}
	if terminal(target.State) || target.State == stateAuthUnknown {
		return s.finishedTarget(ctx, t, c, i, user, service)
	}
	if target.RunID == "" {
		return s.wait()
	}
	var tc checkpoint
	if json.Unmarshal(target.Snapshot, &tc) != nil {
		return s.finish(ctx, t, c, i, stateFailed, stateFailed)
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
	if terminal(run.Status) || run.Status == "succeeded" || run.Status == "canceled" {
		return s.finishedTarget(ctx, t, c, i, user, service)
	}
	if i.CallbackTaskID != uuid.Nil && i.CallbackTaskID != target.ID {
		return s.finish(ctx, t, c, i, stateFailed, "stale")
	}
	applied, err := s.sendControl(ctx, t, &c, i, auth, target.RunID, run)
	if !applied {
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
) error {
	if i.Kind != "message" || c.Control != nil {
		return s.finish(ctx, t, c, i, stateCompleted, "no_active")
	}
	promoted, err := s.store.PromoteMessage(ctx, t.ID, s.owner, t.Fence)
	if err != nil {
		return s.wait()
	}
	// Promotion retains the immutable accepted task ID and request, with a fresh run checkpoint.
	*t = promoted
	return s.runTask(ctx, t, checkpoint{}, i, user, service)
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
	if t.Route == "cancel" {
		_, err := s.loom.Cancel(ctx, auth, runID, t.ID.String())
		if err != nil {
			return false, s.loomError(ctx, t, *c, i, err)
		}
		return true, nil
	}
	if c.Control == nil {
		input, err := controlInput(*t, i, run)
		if err != nil {
			return false, s.finish(ctx, t, *c, i, stateFailed, "stale")
		}
		c.Control = &input
		if err = s.save(ctx, t, *c, nil); err != nil {
			return false, s.wait()
		}
	}
	_, err := s.loom.Input(ctx, auth, runID, t.ID.String(), *c.Control)
	if err != nil {
		return false, s.loomError(ctx, t, *c, i, err)
	}
	return true, nil
}
func controlInput(t db.Task, i intent, run loom.Run) (loom.InputRequest, error) {
	switch {
	case i.Kind == kindApproval:
		a := run.Approval
		if a == nil || i.PromptKey != promptKey(a) || !a.ExpiresAt.After(time.Now()) {
			return loom.InputRequest{}, ErrUnsupportedUpdate
		}
		return loom.InputRequest{Kind: kindApproval, ApprovalID: a.ID, Decision: i.Decision}, nil
	case i.CallbackTaskID != uuid.Nil:
		q := run.Question
		if q == nil || i.PromptKey != promptKey(q) || i.ChoiceIndex >= len(q.Choices) ||
			!q.ExpiresAt.After(time.Now()) {
			return loom.InputRequest{}, ErrUnsupportedUpdate
		}
		return loom.InputRequest{
			Kind:       "answer",
			QuestionID: q.ID,
			ChoiceIDs:  []string{q.Choices[i.ChoiceIndex].ID},
		}, nil
	case run.Question != nil && t.QuestionID != "":
		q := run.Question
		if t.QuestionID != q.ID || !q.AllowText || !q.ExpiresAt.After(time.Now()) {
			return loom.InputRequest{}, ErrUnsupportedUpdate
		}
		return loom.InputRequest{Kind: "answer", QuestionID: q.ID, Text: i.Text}, nil
	case run.Approval != nil || t.QuestionID != "":
		return loom.InputRequest{}, ErrUnsupportedUpdate
	default:
		return loom.InputRequest{Kind: "steering", Text: i.Text}, nil
	}
}
