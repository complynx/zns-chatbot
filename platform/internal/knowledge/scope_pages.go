package knowledge

import (
	"context"
	"encoding/base64"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/core"
)

// Capabilities reports current explicit grants across all scopes, including
// historical events outside the bounded manual navigation list.
type Capabilities struct {
	CanCurate bool `json:"can_curate"`
	CanReview bool `json:"can_review"`
}

type ScopePage struct {
	Items      []Scope `json:"items"`
	More       bool    `json:"more"`
	NextCursor string  `json:"next_cursor"`
}

const knowledgeScopeSelect = `WITH scopes AS (
 SELECT ''::text AS event,'general'::text AS phase
 UNION ALL SELECT id,CASE WHEN finishes_at<=statement_timestamp() THEN 'past' ELSE 'active_or_upcoming' END FROM core.events
) SELECT event,phase,
 EXISTS(SELECT 1 FROM core.knowledge_permissions p WHERE p.scope=scopes.event AND p.actor=$1 AND p.permission='curate'),
 EXISTS(SELECT 1 FROM core.knowledge_permissions p WHERE p.scope=scopes.event AND p.actor=$1 AND p.permission='review')
 FROM scopes `

func (s Service) Capabilities(ctx context.Context, actor string) (Capabilities, error) {
	var result Capabilities
	if err := s.knownActor(ctx, actor); err != nil {
		return result, err
	}
	err := s.DB.QueryRow(ctx, `SELECT
 EXISTS(SELECT 1 FROM core.knowledge_permissions WHERE actor=$1 AND permission='curate'),
 EXISTS(SELECT 1 FROM core.knowledge_permissions WHERE actor=$1 AND permission='review')`, actor).Scan(&result.CanCurate, &result.CanReview)
	return result, core.DatabaseOperationError(err)
}

func (s Service) Scope(ctx context.Context, actor, event string) (Scope, error) {
	var result Scope
	if !validID(event, true) {
		return result, invalid()
	}
	if err := s.knownActor(ctx, actor); err != nil {
		return result, err
	}
	err := s.DB.QueryRow(ctx, knowledgeScopeSelect+` WHERE event=$2`, actor, event).
		Scan(&result.Event, &result.Phase, &result.CanCurate, &result.CanReview)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, missing()
	}
	return result, core.DatabaseOperationError(err)
}

func (s Service) ScopePage(ctx context.Context, actor, cursor string) (ScopePage, error) {
	result := ScopePage{Items: []Scope{}}
	if err := s.knownActor(ctx, actor); err != nil {
		return result, err
	}
	after := ""
	if cursor != "" {
		const maxScopeCursor = 200
		if len(cursor) > maxScopeCursor {
			return result, invalid()
		}
		raw, err := base64.RawURLEncoding.DecodeString(cursor)
		if err != nil || !validID(string(raw), false) {
			return result, invalid()
		}
		after = string(raw)
	}
	rows, err := s.DB.Query(
		ctx,
		knowledgeScopeSelect+` WHERE ($2='' OR event>$2) ORDER BY event LIMIT $3`,
		actor,
		after,
		MaxResults+1,
	)
	if err != nil {
		return result, core.DatabaseOperationError(err)
	}
	defer rows.Close()
	for rows.Next() {
		var scope Scope
		if err = rows.Scan(&scope.Event, &scope.Phase, &scope.CanCurate, &scope.CanReview); err != nil {
			return result, core.DatabaseOperationError(err)
		}
		result.Items = append(result.Items, scope)
	}
	if err = rows.Err(); err != nil {
		return result, core.DatabaseOperationError(err)
	}
	result.More = len(result.Items) > MaxResults
	if result.More {
		result.Items = result.Items[:MaxResults]
		result.NextCursor = base64.RawURLEncoding.EncodeToString([]byte(result.Items[len(result.Items)-1].Event))
	}
	return result, nil
}
