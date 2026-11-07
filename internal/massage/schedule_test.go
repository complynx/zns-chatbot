package massage_test

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/massage"
)

func TestMassageScheduleRejectsOversizedWireAndUTCYearOverflow(t *testing.T) {
	t.Parallel()
	raw, err := json.Marshal(schedule())
	require.NoError(t, err)
	padded := append([]byte{'{'}, bytes.Repeat([]byte{' '}, massage.MaxDocumentBytes)...)
	padded = append(padded, raw[1:]...)
	var decoded massage.ScheduleDocument
	require.ErrorIs(t, json.Unmarshal(padded, &decoded), massage.ErrInvalid)
	for _, start := range []string{"0001-01-01T00:00:00+01:00", "9999-12-31T22:00:00-02:00"} {
		document := schedule()
		document.Windows[0].StartsAt = instant(start)
		document.Windows[0].EndsAt = document.Windows[0].StartsAt.Add(30 * time.Minute)
		document.Specialists[0].WorkSpans[0].StartsAt = document.Windows[0].StartsAt
		document.Specialists[0].WorkSpans[0].EndsAt = document.Windows[0].EndsAt
		_, err = massage.Normalize(document)
		require.ErrorIs(t, err, massage.ErrInvalid, start)
	}
	document := schedule()
	document.Windows[0].StartsAt = document.Windows[0].StartsAt.In(time.FixedZone("offset", 3600))
	normalized, err := massage.Normalize(document)
	require.NoError(t, err)
	again, err := massage.Normalize(normalized)
	require.NoError(t, err)
	require.Equal(t, normalized, again)
}

func TestMassageCandidatesRejectUnserializableLocalYear(t *testing.T) {
	t.Parallel()
	document := schedule()
	start, end := instant("9999-12-31T22:00:00Z"), instant("9999-12-31T23:00:00Z")
	document.Windows[0].StartsAt, document.Windows[0].EndsAt = start, end
	document.Specialists[0].WorkSpans[0].StartsAt, document.Specialists[0].WorkSpans[0].EndsAt = start, end
	request := query()
	request.From, request.Until = start, end
	_, err := massage.CandidateStarts(document, request, start, "Europe/Minsk")
	require.ErrorIs(t, err, massage.ErrInvalid)
	page, err := massage.CandidateStarts(document, request, start, "UTC")
	require.NoError(t, err)
	_, err = json.Marshal(page)
	require.NoError(t, err)
}

func instant(value string) time.Time {
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		panic(err)
	}
	return parsed
}

func schedule() massage.ScheduleDocument {
	start, end := instant("2026-10-24T23:00:00Z"), instant("2026-10-25T03:00:00Z")
	return massage.ScheduleDocument{
		Currency:      "BYN",
		CurrencyScale: 2,
		BlockSeconds:  1200,
		BufferSeconds: 300,
		Services: []massage.ScheduleService{
			{ID: "short", Name: massage.ScheduleLabel{EN: "Short", RU: "Короткий"}, Blocks: 1, PriceMinor: 4500},
			{ID: "long", Name: massage.ScheduleLabel{EN: "Long", RU: "Длинный"}, Blocks: 3, PriceMinor: 9000},
		},
		Windows: []massage.ScheduleWindow{
			{
				ID:       "overnight",
				Name:     massage.ScheduleLabel{EN: "Night", RU: "Ночь"},
				StartsAt: start,
				EndsAt:   end,
				Tables:   1,
			},
		},
		Specialists: []massage.ScheduleSpecialist{{ID: "alex", Name: massage.ScheduleLabel{EN: "Alex", RU: "Алекс"},
			RequiresTable: true, ServiceIDs: []string{"short", "long"},
			WorkSpans: []massage.ScheduleWorkSpan{{WindowID: "overnight", StartsAt: start, EndsAt: end}}}},
	}
}

func query() massage.ScheduleCandidateQuery {
	return massage.ScheduleCandidateQuery{WindowID: "overnight", SpecialistID: "alex", ServiceID: "short",
		From: instant("2026-10-24T23:00:00Z"), Until: instant("2026-10-25T03:00:00Z"), Limit: 100}
}

func TestMassageScheduleKeepsOccupancyBufferAndDSTInstantsDistinct(t *testing.T) {
	t.Parallel()
	document := schedule()
	page, err := massage.CandidateStarts(document, query(), query().From, "Europe/Berlin")
	require.NoError(t, err)
	require.True(t, page.ScheduleOnly)
	require.False(t, page.ReservationSupported)
	require.Equal(t, 1200, page.OccupancySeconds)
	require.Equal(t, 900, page.TreatmentSeconds)
	require.EqualValues(t, 4500, page.PriceMinor)
	require.Len(t, page.Candidates, 12)
	require.True(t, page.Candidates[11].OccupancyEndsAt.Equal(document.Windows[0].EndsAt))
	require.Equal(t, "2026-10-25T02:00:00+02:00", page.Candidates[3].StartsAt.Format(time.RFC3339))
	require.Equal(t, "2026-10-25T02:00:00+01:00", page.Candidates[6].StartsAt.Format(time.RFC3339))
	require.True(t, page.Candidates[6].StartsAt.After(page.Candidates[3].StartsAt))
	for _, candidate := range page.Candidates {
		require.Equal(t, 15*time.Minute, candidate.TreatmentEndsAt.Sub(candidate.StartsAt))
		require.Equal(t, 20*time.Minute, candidate.OccupancyEndsAt.Sub(candidate.StartsAt))
	}
	require.False(t, page.HasMore)
	require.Nil(t, page.NextAfter)
}

func TestScheduleCandidatesFitOneSpanAndRoundUpOnWindowGrid(t *testing.T) {
	t.Parallel()
	document := schedule()
	start := document.Windows[0].StartsAt
	document.Specialists[0].WorkSpans = []massage.ScheduleWorkSpan{
		{WindowID: "overnight", StartsAt: start.Add(5 * time.Minute), EndsAt: start.Add(45 * time.Minute)},
		{WindowID: "overnight", StartsAt: start.Add(45 * time.Minute), EndsAt: start.Add(2 * time.Hour)},
	}
	request := query()
	request.ServiceID = "long"
	page, err := massage.CandidateStarts(document, request, start, "Asia/Yekaterinburg")
	require.NoError(t, err)
	require.Len(t, page.Candidates, 1, "Adjacent spans must not silently become one longer work interval")
	require.True(t, page.Candidates[0].StartsAt.Equal(start.Add(time.Hour)))
	require.Equal(t, 3600, page.OccupancySeconds)
	require.Equal(t, 3300, page.TreatmentSeconds)
	request.ServiceID = "short"
	page, err = massage.CandidateStarts(document, request, start.Add(25*time.Minute), "Asia/Yekaterinburg")
	require.NoError(t, err)
	require.Len(t, page.Candidates, 3)
	require.True(t, page.Candidates[0].StartsAt.Equal(start.Add(time.Hour)))
	require.Equal(t, "+05:00", page.Candidates[0].StartsAt.Format("-07:00"))
}

func TestScheduleCandidatePagesDoNotRepeatOrInventTableCapacity(t *testing.T) {
	t.Parallel()
	document := schedule()
	request := query()
	request.Limit = 2
	var starts []time.Time
	for {
		page, err := massage.CandidateStarts(document, request, request.From, "Europe/Minsk")
		require.NoError(t, err)
		require.LessOrEqual(t, len(page.Candidates), 2)
		for _, candidate := range page.Candidates {
			starts = append(starts, candidate.StartsAt)
		}
		if !page.HasMore {
			require.Nil(t, page.NextAfter)
			break
		}
		require.NotNil(t, page.NextAfter)
		request.AfterStart = page.NextAfter
	}
	require.Len(t, starts, 12)
	for index := 1; index < len(starts); index++ {
		require.True(t, starts[index].After(starts[index-1]))
	}
	document.Windows[0].Tables = 0
	page, err := massage.CandidateStarts(document, query(), request.From, "Europe/Minsk")
	require.NoError(t, err)
	require.Empty(t, page.Candidates)
	document.Specialists[0].RequiresTable = false
	page, err = massage.CandidateStarts(document, query(), request.From, "Europe/Minsk")
	require.NoError(t, err)
	require.Len(t, page.Candidates, 12)
	for _, tables := range []int{1, 2} {
		document.Specialists[0].RequiresTable = true
		document.Windows[0].Tables = tables
		page, err = massage.CandidateStarts(document, query(), request.From, "Europe/Minsk")
		require.NoError(t, err)
		require.Len(t, page.Candidates, 12, "Configured tables are not a claim about live reservations")
		require.False(t, page.ReservationSupported)
	}
}

func TestMassageWireRequiresExplicitFalseAndZeroConfiguration(t *testing.T) {
	t.Parallel()
	document := schedule()
	document.CurrencyScale, document.BufferSeconds, document.Windows[0].Tables = 0, 0, 0
	document.Services[0].PriceMinor = 0
	document.Specialists[0].RequiresTable = false
	raw, err := json.Marshal(document)
	require.NoError(t, err)
	var decoded massage.ScheduleDocument
	require.NoError(t, json.Unmarshal(raw, &decoded))
	_, err = massage.Normalize(decoded)
	require.NoError(t, err)
	for _, field := range []string{"requires_table", "price_minor", "tables", "currency_scale", "buffer_seconds"} {
		var values map[string]any
		require.NoError(t, json.Unmarshal(raw, &values))
		switch field {
		case "requires_table":
			delete(values["specialists"].([]any)[0].(map[string]any), field)
		case "price_minor":
			delete(values["services"].([]any)[0].(map[string]any), field)
		case "tables":
			delete(values["windows"].([]any)[0].(map[string]any), field)
		default:
			delete(values, field)
		}
		missing, marshalErr := json.Marshal(values)
		require.NoError(t, marshalErr)
		require.Error(t, json.Unmarshal(missing, &decoded), field)
	}
}

func TestMassageScheduleRejectsContradictoryReferencesBoundsAndOverlaps(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		change func(*massage.ScheduleDocument)
	}{
		{"unknown service", func(d *massage.ScheduleDocument) { d.Specialists[0].ServiceIDs = []string{"missing"} }},
		{"unknown window", func(d *massage.ScheduleDocument) { d.Specialists[0].WorkSpans[0].WindowID = "missing" }},
		{"work outside window", func(d *massage.ScheduleDocument) {
			d.Specialists[0].WorkSpans[0].StartsAt = d.Windows[0].StartsAt.Add(-time.Minute)
		}},
		{"overlap", func(d *massage.ScheduleDocument) {
			d.Specialists[0].WorkSpans = append(d.Specialists[0].WorkSpans, d.Specialists[0].WorkSpans[0])
		}},
		{
			"window too long",
			func(d *massage.ScheduleDocument) { d.Windows[0].EndsAt = d.Windows[0].StartsAt.Add(49 * time.Hour) },
		},
		{"buffer consumes block", func(d *massage.ScheduleDocument) { d.BufferSeconds = d.BlockSeconds }},
		{"block too small", func(d *massage.ScheduleDocument) { d.BlockSeconds = 1 }},
		{"duplicate service", func(d *massage.ScheduleDocument) { d.Services = append(d.Services, d.Services[0]) }},
		{"oversized price", func(d *massage.ScheduleDocument) { d.Services[0].PriceMinor = massage.MaxPrice + 1 }},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			t.Parallel()
			document := schedule()
			item.change(&document)
			_, err := massage.Normalize(document)
			require.ErrorIs(t, err, massage.ErrInvalid)
		})
	}
	document, err := massage.Normalize(schedule())
	require.NoError(t, err)
	require.True(t, massage.FitsEvent(document, document.Windows[0].StartsAt, document.Windows[0].EndsAt))
	require.False(
		t,
		massage.FitsEvent(document, document.Windows[0].StartsAt.Add(time.Minute), document.Windows[0].EndsAt),
	)
	request := query()
	request.Until = request.From.Add(25 * time.Hour)
	_, err = massage.CandidateStarts(document, request, request.From, "Europe/Minsk")
	require.ErrorIs(t, err, massage.ErrInvalid)
	request = query()
	request.Limit = 101
	_, err = massage.CandidateStarts(document, request, request.From, "Europe/Minsk")
	require.ErrorIs(t, err, massage.ErrInvalid)
}
