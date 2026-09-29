package passbooking

// MaxAdminBatchRecipients bounds synchronous work and result size.
const MaxAdminBatchRecipients = 100

// AdminCancellation carries the same optimistic versions as Execute.
type AdminCancellation struct {
	Event         string `json:"event"`
	Key           string `json:"key"`
	Version       int64  `json:"version"`
	Target        string `json:"target"`
	TargetVersion int64  `json:"target_version"`
}

func (c AdminCancellation) command() Command {
	return Command{Name: commandAdminCancel, Event: c.Event, Key: c.Key,
		Version: c.Version, Target: c.Target, TargetVersion: c.TargetVersion}
}

type AdminBatchStatus string

const (
	AdminBatchSucceeded    AdminBatchStatus = "succeeded"
	AdminBatchRejected     AdminBatchStatus = "rejected"
	AdminBatchNotAttempted AdminBatchStatus = "not_attempted"
	// AdminBatchInterrupted requires retrying the same key to resolve commit uncertainty.
	AdminBatchInterrupted AdminBatchStatus = "interrupted"
)

// AdminBatchOutcome follows input order. Assignment is populated only on success
// for assignment batches. Code is a public domain error, never a database error.
type AdminBatchOutcome struct {
	Target     string                 `json:"target"`
	Key        string                 `json:"key"`
	Status     AdminBatchStatus       `json:"status"`
	Code       string                 `json:"code,omitempty"`
	Assignment *AdminAssignmentResult `json:"assignment,omitempty"`
}

func validateAdminBatch(items []AdminBatchOutcome, events []string) error {
	if len(items) == 0 || len(items) > MaxAdminBatchRecipients {
		return invalid()
	}
	keys, targets := map[string]bool{}, map[string]bool{}
	for i, item := range items {
		if events[i] != events[0] || keys[item.Key] || targets[item.Target] {
			return invalid()
		}
		keys[item.Key], targets[item.Target] = true, true
	}
	return nil
}
