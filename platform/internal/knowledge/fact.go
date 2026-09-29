package knowledge

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

// Fact returns the exact scoped record, including an inactive version needed
// for safe replacement. It does not substitute historical or general facts.
func (s Service) Fact(ctx context.Context, actor, event, topic, key string) (Fact, error) {
	if !validID(event, true) || !validID(topic, false) || !validID(key, false) {
		return Fact{}, invalid()
	}
	if err := s.knownActor(ctx, actor); err != nil {
		return Fact{}, err
	}
	result := Fact{Event: event, Topic: topic, Key: key, Untrusted: true}
	err := s.DB.QueryRow(ctx, `SELECT f.body,f.version,f.active,
 CASE WHEN f.scope='' THEN 'general' WHEN e.finishes_at<=statement_timestamp() THEN 'past' ELSE 'active_or_upcoming' END
 FROM core.knowledge_facts f LEFT JOIN core.events e ON e.id=f.scope WHERE f.scope=$1 AND f.topic=$2 AND f.fact_key=$3`, event, topic, key).
		Scan(&result.Text, &result.Version, &result.Active, &result.Phase)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, nil
	}
	if err != nil {
		return result, err
	}
	entry, err := s.authorizeMemoryEntry(ctx, actor, factMemoryEntry(result))
	result.ReadAuthorities = entry.ReadAuthorities
	return result, err
}
