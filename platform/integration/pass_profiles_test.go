package integration_test

import (
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/passes"
)

func profileCommand(name, field, key string, p passes.Profile) passes.Command {
	return passes.Command{Name: name, Field: field, Version: p.Version, Key: key, Origin: "manual"}
}

func TestPassProfileIdentityRestartAndIsolation(t *testing.T) {
	t.Parallel()
	db := database(t)
	s := passes.Service{DB: db}
	p, err := s.Get(t.Context(), "alice")
	require.NoError(t, err)
	assert.Zero(t, p.Version)
	begin := profileCommand("begin", "legal_name", "begin", p)
	begin.PassportAfter = true
	p, err = s.Execute(t.Context(), "alice", begin)
	require.NoError(t, err)
	s = passes.Service{DB: db}
	restarted, err := s.Get(t.Context(), "alice")
	require.NoError(t, err)
	assert.Equal(t, p, restarted)
	submit := profileCommand("submit", "legal_name", "name", p)
	submit.Value = "  王小明 / O'Neill — Иван  "
	p, err = s.Execute(t.Context(), "alice", submit)
	require.NoError(t, err)
	assert.Equal(t, "王小明 / O'Neill — Иван", p.LegalName)
	assert.Equal(t, "passport", p.Pending)
	passport := profileCommand("submit", "passport", "passport", p)
	passport.Value = "TEST ONLY 12345"
	passport.Origin = "agent"
	p, err = s.Execute(t.Context(), "alice", passport)
	require.NoError(t, err)
	assert.Equal(t, "TEST ONLY 12345", p.Passport)
	assert.Empty(t, p.Pending)
	assert.Nil(t, p.ExpiresAt)
	bob, err := s.Get(t.Context(), "bob")
	require.NoError(t, err)
	assert.Empty(t, bob.LegalName)
	assert.Empty(t, bob.Passport)
	_, err = s.Execute(t.Context(), "bob", passport)
	requireCode(t, err, "pass_profile_stale")
	replayed, err := s.Execute(t.Context(), "alice", begin)
	require.NoError(t, err)
	assert.Equal(t, p, replayed)
	_, err = s.Execute(t.Context(), "alice", submit)
	require.NoError(t, err)
	passport.Value = "different"
	_, err = s.Execute(t.Context(), "alice", passport)
	requireCode(t, err, "idempotency_conflict")
	var operations string
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT json_agg(o)::text FROM core.pass_profile_operations o`).Scan(&operations),
	)
	assert.NotContains(t, operations, p.Passport)
	assert.NotContains(t, operations, p.LegalName)
}

func TestPassProfileCancelExpiryAndDenial(t *testing.T) {
	t.Parallel()
	db := database(t)
	s := passes.Service{DB: db}
	p, err := s.Execute(t.Context(), "alice", profileCommand("begin", "legal_name", "begin", passes.Profile{}))
	require.NoError(t, err)
	old := profileCommand("submit", "legal_name", "old", p)
	old.Value = "Old Input"
	p, err = s.Execute(t.Context(), "alice", profileCommand("cancel", "", "cancel", p))
	require.NoError(t, err)
	_, err = s.Execute(t.Context(), "alice", old)
	requireCode(t, err, "pass_profile_stale")
	p, err = s.Execute(t.Context(), "alice", profileCommand("begin", "legal_name", "again", p))
	require.NoError(t, err)
	_, err = db.Exec(
		t.Context(),
		`UPDATE core.pass_profiles SET expires_at=clock_timestamp()-interval '1 second' WHERE owner='alice'`,
	)
	require.NoError(t, err)
	expired, err := s.Get(t.Context(), "alice")
	require.NoError(t, err)
	old.Version = p.Version
	_, err = s.Execute(t.Context(), "alice", old)
	requireCode(t, err, "pass_profile_expired")
	after, err := s.Get(t.Context(), "alice")
	require.NoError(t, err)
	assert.Equal(t, expired, after)
	p, err = s.Execute(t.Context(), "alice", profileCommand("begin", "role", "role", p))
	require.NoError(t, err)
	submit := profileCommand("submit", "role", "submit", p)
	submit.Value = "leader"
	_, err = db.Exec(t.Context(), `UPDATE core.users SET can_book=false WHERE id='alice'`)
	require.NoError(t, err)
	_, err = s.Execute(t.Context(), "alice", submit)
	requireCode(t, err, "forbidden")
	after, err = s.Get(t.Context(), "alice")
	require.NoError(t, err)
	assert.Equal(t, p, after)
	_, err = s.Get(t.Context(), "unknown")
	requireCode(t, err, "forbidden")
}

func TestPassProfileFrozenIdentityAllowsRole(t *testing.T) {
	t.Parallel()
	db := database(t)
	s := passes.Service{DB: db}
	_, err := db.Exec(
		t.Context(),
		`INSERT INTO core.pass_profiles(owner,legal_name,passport,frozen) VALUES('alice','Frozen Name','TEST FROZEN',true)`,
	)
	require.NoError(t, err)
	p, err := s.Get(t.Context(), "alice")
	require.NoError(t, err)
	for _, field := range []string{"legal_name", "passport"} {
		_, err = s.Execute(t.Context(), "alice", profileCommand("begin", field, field, p))
		requireCode(t, err, "pass_profile_frozen")
	}
	unchanged, err := s.Get(t.Context(), "alice")
	require.NoError(t, err)
	assert.Equal(t, p, unchanged)
	p, err = s.Execute(t.Context(), "alice", profileCommand("begin", "role", "role", p))
	require.NoError(t, err)
	submit := profileCommand("submit", "role", "select", p)
	submit.Value = "follower"
	p, err = s.Execute(t.Context(), "alice", submit)
	require.NoError(t, err)
	assert.Equal(t, "follower", p.Role)
	assert.Equal(t, "Frozen Name", p.LegalName)
	assert.Equal(t, "TEST FROZEN", p.Passport)
}

func TestPassProfileConcurrentSubmitAndInvalidInput(t *testing.T) {
	t.Parallel()
	db := database(t)
	s := passes.Service{DB: db}
	p, err := s.Execute(t.Context(), "alice", profileCommand("begin", "legal_name", "begin", passes.Profile{}))
	require.NoError(t, err)
	for _, value := range []string{"", "  ", string([]byte{0xff}), "a\x00b", strings.Repeat("界", 301)} {
		invalid := profileCommand("submit", "legal_name", "invalid", p)
		invalid.Value = value
		_, err = s.Execute(t.Context(), "alice", invalid)
		requireCode(t, err, "pass_profile_invalid")
	}
	unchanged, err := s.Get(t.Context(), "alice")
	require.NoError(t, err)
	assert.Equal(t, p, unchanged)
	var wg sync.WaitGroup
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, key := range []string{"first", "second"} {
		wg.Go(func() {
			<-start
			c := profileCommand("submit", "legal_name", key, p)
			c.Value = key
			_, submitErr := s.Execute(t.Context(), "alice", c)
			results <- submitErr
		})
	}
	close(start)
	wg.Wait()
	close(results)
	success := 0
	for result := range results {
		if result == nil {
			success++
		} else {
			requireCode(t, result, "pass_profile_stale")
		}
	}
	assert.Equal(t, 1, success)
	after, err := s.Get(t.Context(), "alice")
	require.NoError(t, err)
	assert.Equal(t, p.Version+1, after.Version)
	assert.Contains(t, []string{"first", "second"}, after.LegalName)
}

func TestPassProfileDuplicateRaceAndFreezeDuringInput(t *testing.T) {
	t.Parallel()
	db := database(t)
	s := passes.Service{DB: db}
	p, err := s.Execute(t.Context(), "alice", profileCommand("begin", "passport", "begin", passes.Profile{}))
	require.NoError(t, err)
	submit := profileCommand("submit", "passport", "same-submit", p)
	submit.Value = "TEST DUPLICATE"
	var wg sync.WaitGroup
	start := make(chan struct{})
	results := make(chan error, 2)
	for range 2 {
		wg.Go(func() {
			<-start
			_, submitErr := s.Execute(t.Context(), "alice", submit)
			results <- submitErr
		})
	}
	close(start)
	wg.Wait()
	close(results)
	for result := range results {
		require.NoError(t, result)
	}
	after, err := s.Get(t.Context(), "alice")
	require.NoError(t, err)
	assert.Equal(t, p.Version+1, after.Version)
	assert.Equal(t, submit.Value, after.Passport)
	p, err = s.Execute(t.Context(), "alice", profileCommand("begin", "passport", "again", after))
	require.NoError(t, err)
	_, err = db.Exec(t.Context(), `UPDATE core.pass_profiles SET frozen=true WHERE owner='alice'`)
	require.NoError(t, err)
	p.Frozen = true
	submit = profileCommand("submit", "passport", "frozen-submit", p)
	submit.Value = "CHANGED"
	_, err = s.Execute(t.Context(), "alice", submit)
	requireCode(t, err, "pass_profile_frozen")
	after, err = s.Get(t.Context(), "alice")
	require.NoError(t, err)
	assert.Equal(t, p, after)
	p, err = s.Execute(t.Context(), "alice", profileCommand("cancel", "", "cancel", p))
	require.NoError(t, err)
	assert.Empty(t, p.Pending)
	assert.Equal(t, "TEST DUPLICATE", p.Passport)
}

func TestPassProfileExplicitSetWithoutPromptAndWithInterruption(t *testing.T) {
	t.Parallel()
	db := database(t)
	s := passes.Service{DB: db}
	set := profileCommand("set", "legal_name", "unsolicited-name", passes.Profile{})
	set.Origin = "agent"
	set.Value = "Иван O'Neill"
	p, err := s.Execute(t.Context(), "alice", set)
	require.NoError(t, err)
	assert.Equal(t, set.Value, p.LegalName)
	assert.Empty(t, p.Pending)
	replayed, err := s.Execute(t.Context(), "alice", set)
	require.NoError(t, err)
	assert.Equal(t, p, replayed)
	set.Value = "Different"
	_, err = s.Execute(t.Context(), "alice", set)
	requireCode(t, err, "idempotency_conflict")
	begin := profileCommand("begin", "legal_name", "request-name", p)
	begin.PassportAfter = true
	p, err = s.Execute(t.Context(), "alice", begin)
	require.NoError(t, err)
	// Reading context to answer an unrelated question must not consume the hint.
	contextProfile, err := s.Get(t.Context(), "alice")
	require.NoError(t, err)
	assert.Equal(t, p, contextProfile)
	role := profileCommand("set", "role", "unrelated-role", p)
	role.Origin = "agent"
	role.Value = "follower"
	changed, err := s.Execute(t.Context(), "alice", role)
	require.NoError(t, err)
	assert.Equal(t, "follower", changed.Role)
	assert.Equal(t, p.Pending, changed.Pending)
	assert.Equal(t, p.ExpiresAt, changed.ExpiresAt)
	assert.Equal(t, p.PassportAfter, changed.PassportAfter)
	set = profileCommand("set", "legal_name", "explicit-answer", changed)
	set.Origin = "agent"
	set.Value = "王小明"
	p, err = s.Execute(t.Context(), "alice", set)
	require.NoError(t, err)
	assert.Equal(t, "王小明", p.LegalName)
	assert.Equal(t, "passport", p.Pending)
	assert.False(t, p.PassportAfter)
	for _, origin := range []string{"agent", "manual"} {
		set = profileCommand("set", "passport", "passport-"+origin, p)
		set.Origin = origin
		set.Value = "TEST ONLY " + origin
		p, err = s.Execute(t.Context(), "alice", set)
		require.NoError(t, err)
		assert.Equal(t, set.Value, p.Passport)
		assert.Empty(t, p.Pending)
	}
}

func TestPassProfileOriginHasEquivalentPassportPermissions(t *testing.T) {
	t.Parallel()
	for _, origin := range []string{"manual", "agent"} {
		t.Run(origin, func(t *testing.T) {
			t.Parallel()
			db := database(t)
			s := passes.Service{DB: db}
			begin := profileCommand("begin", "passport", "begin", passes.Profile{})
			begin.Origin = origin
			p, err := s.Execute(t.Context(), "alice", begin)
			require.NoError(t, err)
			submit := profileCommand("submit", "passport", "submit", p)
			submit.Origin = origin
			submit.Value = "TEST EQUIVALENCE"
			p, err = s.Execute(t.Context(), "alice", submit)
			require.NoError(t, err)
			assert.Equal(t, submit.Value, p.Passport)
			_, err = db.Exec(t.Context(), `UPDATE core.pass_profiles SET frozen=true WHERE owner='alice'`)
			require.NoError(t, err)
			p.Frozen = true
			set := profileCommand("set", "passport", "locked", p)
			set.Origin = origin
			set.Value = "REPLACEMENT"
			_, err = s.Execute(t.Context(), "alice", set)
			requireCode(t, err, "pass_profile_frozen")
			unchanged, err := s.Get(t.Context(), "alice")
			require.NoError(t, err)
			assert.Equal(t, p, unchanged)
			_, err = db.Exec(t.Context(), `UPDATE core.users SET can_book=false WHERE id='alice'`)
			require.NoError(t, err)
			_, err = s.Execute(t.Context(), "alice", set)
			requireCode(t, err, "forbidden")
		})
	}
}

func TestPassProfileExplicitSetAfterExpiryAndFrozenDenial(t *testing.T) {
	t.Parallel()
	db := database(t)
	s := passes.Service{DB: db}
	begin := profileCommand("begin", "legal_name", "begin", passes.Profile{})
	begin.PassportAfter = true
	p, err := s.Execute(t.Context(), "alice", begin)
	require.NoError(t, err)
	_, err = db.Exec(
		t.Context(),
		`UPDATE core.pass_profiles SET expires_at=clock_timestamp()-interval '1 second' WHERE owner='alice'`,
	)
	require.NoError(t, err)
	set := profileCommand("set", "legal_name", "new-explicit-name", p)
	set.Origin = "agent"
	set.Value = "Fresh Explicit Name"
	p, err = s.Execute(t.Context(), "alice", set)
	require.NoError(t, err)
	assert.Equal(t, set.Value, p.LegalName)
	assert.Empty(t, p.Pending)
	assert.Nil(t, p.ExpiresAt)
	assert.False(t, p.PassportAfter)
	_, err = db.Exec(t.Context(), `UPDATE core.pass_profiles SET frozen=true WHERE owner='alice'`)
	require.NoError(t, err)
	p.Frozen = true
	set = profileCommand("set", "legal_name", "frozen-change", p)
	set.Origin = "agent"
	set.Value = "Replacement"
	_, err = s.Execute(t.Context(), "alice", set)
	requireCode(t, err, "pass_profile_frozen")
	unchanged, err := s.Get(t.Context(), "alice")
	require.NoError(t, err)
	assert.Equal(t, p, unchanged)
	role := profileCommand("set", "role", "frozen-role", p)
	role.Origin = "agent"
	role.Value = "leader"
	p, err = s.Execute(t.Context(), "alice", role)
	require.NoError(t, err)
	assert.Equal(t, "leader", p.Role)
	assert.Equal(t, "Fresh Explicit Name", p.LegalName)
}
