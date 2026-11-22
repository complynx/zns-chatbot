package registrations

// UsesPassInvites selects event-specific invitation codes for sales configured through /passes.
func (r Rules) UsesPassInvites() bool {
	return r.AutoAssign || r.SalesOpensAt != nil
}

// PassQueueRules configures automatic sale allocation for an event. A zero percentage
// uses the original event default; explicit limits can be different per role.
type PassQueueRules struct {
	AutoAssign         bool `json:"auto_assign,omitempty"`
	MaxLeaderPercent   int  `json:"max_leader_percent,omitempty"`
	MaxFollowerPercent int  `json:"max_follower_percent,omitempty"`
}

func (r PassQueueRules) Valid() bool {
	valid := func(limit int) bool { return limit == 0 || (limit >= 50 && limit <= 100) }
	return valid(r.MaxLeaderPercent) && valid(r.MaxFollowerPercent)
}

func (r PassQueueRules) Allows(leaders, followers, addLeaders, addFollowers int64) bool {
	total := leaders + followers
	next := total + addLeaders + addFollowers
	check := func(before, after int64, limit int) bool {
		if limit == 0 {
			limit = 52
		}
		return after*100 <= int64(limit)*next || (total > 0 && after*total < before*next)
	}
	return check(leaders, leaders+addLeaders, r.MaxLeaderPercent) &&
		check(followers, followers+addFollowers, r.MaxFollowerPercent)
}
