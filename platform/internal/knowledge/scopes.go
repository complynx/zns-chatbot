package knowledge

import "context"

// Scopes reports current explicit grants for GUI/tool discovery. A displayed
// capability is not authorization; every later mutation checks the grant again.
func (s Service) Scopes(ctx context.Context, actor string) ([]Scope, error) {
	if err := s.knownActor(ctx, actor); err != nil {
		return nil, err
	}
	rows, err := s.DB.Query(ctx, `WITH scopes AS (
 SELECT ''::text AS event,'general'::text AS phase,0 AS rank,NULL::timestamptz AS finishes_at
 UNION ALL SELECT id,CASE WHEN finishes_at<=statement_timestamp() THEN 'past' ELSE 'active_or_upcoming' END,
 CASE WHEN finishes_at<=statement_timestamp() THEN 2 ELSE 1 END,finishes_at FROM core.events
 ) SELECT event,phase,
 EXISTS(SELECT 1 FROM core.knowledge_permissions p WHERE p.scope=scopes.event AND p.actor=$1 AND p.permission='curate'),
 EXISTS(SELECT 1 FROM core.knowledge_permissions p WHERE p.scope=scopes.event AND p.actor=$1 AND p.permission='review')
 FROM scopes ORDER BY rank,finishes_at DESC NULLS FIRST,event LIMIT $2`, actor, MaxResults)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]Scope, 0)
	for rows.Next() {
		var scope Scope
		if err = rows.Scan(&scope.Event, &scope.Phase, &scope.CanCurate, &scope.CanReview); err != nil {
			return nil, err
		}
		result = append(result, scope)
	}
	return result, rows.Err()
}
