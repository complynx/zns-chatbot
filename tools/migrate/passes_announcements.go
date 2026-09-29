package migrate

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

func validateAnnouncementPolicy(p preparedPasses) error {
	if p.HistoricalAnnouncements != "" && p.HistoricalAnnouncements != "suppress_historical" &&
		p.HistoricalAnnouncements != "preserve_source_eligibility" {
		return errors.New("pass_announcement_policy_invalid")
	}
	for _, rows := range p.Bookings {
		for _, row := range rows {
			if !row.Shadowed && len(row.Candidate.AnnouncementMarker) == 0 && p.HistoricalAnnouncements == "" {
				return errors.New("pass_announcement_policy_required")
			}
		}
	}
	return nil
}

func insertPassAnnouncements(ctx context.Context, tx pgx.Tx, p preparedPasses, owners map[string]string) error {
	for _, event := range p.eventNames() {
		for _, row := range p.Bookings[event] {
			if row.Shadowed {
				continue
			}
			b := row.Candidate
			present := len(b.AnnouncementMarker) > 0
			policy := p.HistoricalAnnouncements
			if present {
				policy = "source_marker"
			}
			owner := passOwner(owners, b.TelegramID)
			if _, err := tx.Exec(
				ctx,
				`INSERT INTO core.legacy_pass_announcement_metadata(source_key,event_id,owner,created_at,marker_present,source_marker,policy) VALUES($1,$2,$3,$4,$5,$6,$7)`,
				row.Legacy.Key,
				b.Event,
				owner,
				b.Created,
				present,
				b.AnnouncementMarker,
				policy,
			); err != nil {
				return errors.New("apply_announcement_conflict")
			}
			if _, err := tx.Exec(
				ctx,
				`INSERT INTO core.pass_registration_announcements(event_id,owner,created_at,channel,thread_id,locale,name,role,state,failure)
 SELECT $1,$2,$3,e.thread_channel,e.thread_id,e.thread_locale,u.name,$4,
 CASE WHEN $5='preserve_source_eligibility' AND e.thread_channel<>'' AND (e.open_ended OR e.finishes_at>clock_timestamp()) THEN 'pending' ELSE 'suppressed' END,$5
 FROM core.pass_events e JOIN core.users u ON u.id=$2 WHERE e.id=$1`,
				b.Event,
				owner,
				b.Created,
				b.Role,
				policy,
			); err != nil {
				return errors.New("apply_announcement_conflict")
			}
		}
	}
	return nil
}
