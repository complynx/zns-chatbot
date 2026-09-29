package massage_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/massage"
)

func fixtureParty() massage.Party {
	start := time.Date(2026, time.September, 25, 18, 0, 0, 0, time.FixedZone("Europe/Minsk", 3*60*60))
	return massage.Party{Start: start, End: start.Add(4 * time.Hour), Tables: 1}
}

func TestQuotes(t *testing.T) {
	t.Parallel()
	for index, byn := range []int{43, 57, 90, 110, 125, 150} {
		duration, price, err := massage.Quote(index+1, massage.BYN)
		require.NoError(t, err)
		assert.Equal(t, byn, price)
		assert.Equal(t, time.Duration((index+1)*20-5)*time.Minute, duration)
	}
	for index, rub := range []int{1200, 1600, 2500, 3000, 3500, 4000} {
		_, price, err := massage.Quote(index+1, massage.RUB)
		require.NoError(t, err)
		assert.Equal(t, rub, price)
	}
	_, _, err := massage.Quote(7, massage.BYN)
	require.ErrorIs(t, err, massage.ErrLength)
	_, _, err = massage.Quote(1, "EUR")
	require.ErrorIs(t, err, massage.ErrCurrency)
}

func TestWorkSlots(t *testing.T) {
	t.Parallel()
	party := fixtureParty()
	spans := []massage.Span{
		{Start: party.Start.Add(-121 * time.Minute), End: party.End}, // rejected by span start
		{Start: party.Start.Add(-39 * time.Minute), End: party.Start.Add(41 * time.Minute)},
		{Start: party.End, End: party.End.Add(40 * time.Minute)}, // not clipped
	}
	assert.Equal(t, map[int]bool{-1: true, 0: true, 1: true, 12: true, 13: true}, massage.WorkSlots(party, spans))
}

func TestAvailability(t *testing.T) {
	t.Parallel()
	party := fixtureParty()
	work := []massage.Span{{Start: party.Start, End: party.Start.Add(time.Hour)}}
	specialists := []massage.Specialist{
		{ID: 1, LegacyTableFlag: true, Work: work},
		{ID: 2, LegacyTableFlag: true, Work: work},
		{ID: 3, Work: work},
	}
	bookings := []massage.Booking{{SpecialistID: 1, Slot: 1, Length: 1}}
	slots, err := massage.Available(party, specialists, bookings, 1, party.Start)
	require.NoError(t, err)
	assert.Equal(t, map[int64]bool{3: true}, slots[1])
	assert.Len(t, slots[0], 3)
	slots, err = massage.Available(party, specialists, bookings, 2, party.Start)
	require.NoError(t, err)
	assert.Equal(t, massage.Slots{0: {3: true}, 1: {3: true}}, slots)
	party.Tables = 2
	specialists = append(specialists, massage.Specialist{ID: 4, LegacyTableFlag: true, Work: work})
	_, err = massage.Available(party, specialists, bookings, 1, party.Start)
	require.ErrorIs(t, err, massage.ErrTables)
}

func TestPartyAndInstantBoundaries(t *testing.T) {
	t.Parallel()
	party := fixtureParty()
	_, found := massage.CurrentParty([]massage.Party{party}, party.Start.Add(-2*time.Hour))
	assert.False(t, found)
	_, found = massage.CurrentParty([]massage.Party{party}, party.End.Add(2*time.Hour))
	assert.False(t, found)
	_, found = massage.CurrentParty([]massage.Party{party}, party.Start.Add(-time.Hour))
	assert.True(t, found)
	assert.Equal(t, -1, massage.InstantSlot(party, party.Start.Add(-20*time.Minute)))
	assert.Equal(t, 0, massage.InstantSlot(party, party.Start.Add(5*time.Minute)))
	assert.Equal(t, 1, massage.InstantSlot(party, party.Start.Add(5*time.Minute+time.Nanosecond)))
	slots, err := massage.Available(party, nil, nil, 1, party.End.Add(2*time.Hour+time.Nanosecond))
	require.NoError(t, err)
	assert.Empty(t, slots)
}

func TestEligibility(t *testing.T) {
	t.Parallel()
	party := fixtureParty()
	request := massage.Request{Slot: 1, Length: 1, SpecialistID: 1, DailyLimit: 3}
	available := massage.Slots{1: {1: true, 2: true}}
	now := party.Start.Add(5 * time.Minute)
	assert.Equal(t, massage.Allowed, massage.Eligibility(party, now, request, available, nil))
	assert.Equal(t, massage.Timeout, massage.Eligibility(party, now.Add(time.Nanosecond), request, available, nil))
	bookings := []massage.Booking{{Slot: 2, Length: 1}}
	assert.Equal(t, massage.ClientConflict, massage.Eligibility(party, now, request, available, bookings))
	request.SpecialistID = 9
	assert.Equal(t, massage.Unavailable, massage.Eligibility(party, now, request, available, bookings))
	request.DailyLimit = 1
	assert.Equal(t, massage.DailyLimit, massage.Eligibility(party, now, request, available, bookings))
	request.ActorIsSpecialist = true
	request.SpecialistID = 1
	assert.Equal(
		t,
		massage.Allowed,
		massage.Eligibility(party, party.Start.Add(25*time.Minute), request, available, bookings),
	)
	assert.Equal(
		t,
		massage.Timeout,
		massage.Eligibility(party, party.Start.Add(25*time.Minute+time.Nanosecond), request, available, bookings),
	)
}
