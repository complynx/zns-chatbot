package scriptprotocol

import "time"

// ActiveTime bounds cumulative VM-active elapsed time, excluding host waits.
// It includes serialization and scheduling delay; it is not a CPU-time budget.
const ActiveTime = 200 * time.Millisecond

// Host operations have their own bound inside one shared execution deadline.
// A callback cannot renew the run, and an earlier trusted parent deadline wins.
// Process and transport margins allow timeout delivery and child cleanup.
const (
	HostCallTimeout         = 10 * time.Second
	ExecuteTimeout          = 60 * time.Second
	ExecuteProcessTimeout   = ExecuteTimeout + time.Second
	ExecuteTransportTimeout = ExecuteProcessTimeout + time.Second
	EvaluateProcessTimeout  = 2 * time.Second
	EvaluateHostTimeout     = EvaluateProcessTimeout + time.Second
	EvaluateClientTimeout   = 5 * time.Second
	ProcessWaitDelay        = time.Second
)
