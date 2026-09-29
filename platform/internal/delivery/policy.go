package delivery

import "time"

// Deadline uses integer seconds without converting a Telegram delay to Duration.
// The bound is the RFC3339 representation used by public queue observations.
// An unrepresentable deadline must be parked, never shortened to this bound.
func Deadline(now time.Time, seconds int64) (time.Time, bool) {
	maximum := time.Date(9999, time.December, 31, 23, 59, 59, 999999999, time.UTC)
	if seconds < 0 || now.After(maximum) || seconds > maximum.Unix()-now.Unix() {
		return time.Time{}, false
	}
	return time.Unix(now.Unix()+seconds, int64(now.Nanosecond())).UTC(), true
}

func (s Settings) Cooldown(now time.Time, outcome Outcome) (time.Time, bool) {
	if outcome.Kind != Deferred || !outcome.Valid() {
		return time.Time{}, false
	}
	seconds := outcome.RetryAfter
	if outcome.Missing {
		seconds = int64(s.Fallback / time.Second)
		if s.Fallback%time.Second != 0 {
			seconds++
		}
	}
	return Deadline(now, seconds)
}
