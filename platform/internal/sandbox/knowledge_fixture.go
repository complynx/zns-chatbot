package sandbox

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
)

const knowledgeFixtureActor = "bob"
const knowledgeFixtureTimeout = 30 * time.Second

// KnowledgeFixture changes one synthetic ACL leaf, never proposal consent or content.
type KnowledgeFixture struct {
	Stand      string
	Action     string
	Scope      string
	Permission string
}

func (f KnowledgeFixture) Validate() error {
	if f.Stand != RegistrationFixtureStand {
		return errors.New("knowledge fixture stand guard failed")
	}
	if f.Action != clockActionRead && f.Action != "grant" && f.Action != "revoke" {
		return errors.New("unknown knowledge fixture action")
	}
	if f.Scope != "" && f.Scope != "sandbox-festival" && f.Scope != "sandbox-past" {
		return errors.New("knowledge fixture scope guard failed")
	}
	if f.Permission != "review" && f.Permission != "curate" {
		return errors.New("knowledge fixture permission guard failed")
	}
	return nil
}

type KnowledgeFixtureState struct {
	ObservedAt time.Time       `json:"observed_at"`
	Stand      string          `json:"stand"`
	Database   string          `json:"database"`
	Actor      string          `json:"actor"`
	Scope      knowledge.Scope `json:"scope"`
}

// ApplyKnowledgeFixture requires the existing private operator and initialized
// registration allocation. Domain actor/scope locks fence even absent grants.
func ApplyKnowledgeFixture(ctx context.Context, db *pgxpool.Pool, f KnowledgeFixture) (KnowledgeFixtureState, error) {
	var state KnowledgeFixtureState
	if err := f.Validate(); err != nil {
		return state, err
	}
	ctx, cancel := context.WithTimeout(ctx, knowledgeFixtureTimeout)
	defer cancel()
	tx, err := db.Begin(ctx)
	if err != nil {
		return state, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(918431003)`); err != nil {
		return state, err
	}
	if err = registrationFixtureOperatorGuard(
		ctx,
		tx,
		RegistrationFixture{Stand: f.Stand, Action: clockActionRead},
	); err != nil {
		return state, err
	}
	if err = knowledgeFixtureGuard(ctx, tx); err != nil {
		return state, err
	}
	if _, err = tx.Exec(ctx, `SELECT id FROM core.users WHERE id='bob' FOR NO KEY UPDATE`); err != nil {
		return state, err
	}
	// Derived knowledge operations lock the shared gate before a destination.
	var scope string
	if err = tx.QueryRow(ctx, `SELECT scope FROM core.knowledge_scopes WHERE scope='' FOR UPDATE`).
		Scan(&scope); err != nil {
		return state, err
	}
	if f.Scope != "" {
		if err = tx.QueryRow(ctx, `SELECT scope FROM core.knowledge_scopes WHERE scope=$1 FOR UPDATE`, f.Scope).
			Scan(&scope); err != nil {
			return state, err
		}
	}
	switch f.Action {
	case "grant":
		_, err = tx.Exec(
			ctx,
			`INSERT INTO core.knowledge_permissions(scope,actor,permission) VALUES($1,'bob',$2) ON CONFLICT DO NOTHING`,
			f.Scope,
			f.Permission,
		)
	case "revoke":
		_, err = tx.Exec(
			ctx,
			`DELETE FROM core.knowledge_permissions WHERE scope=$1 AND actor='bob' AND permission=$2`,
			f.Scope,
			f.Permission,
		)
	}
	if err != nil {
		return state, err
	}
	if err = tx.Commit(ctx); err != nil {
		return state, err
	}
	current, err := (knowledge.Service{DB: db}).Scope(ctx, knowledgeFixtureActor, f.Scope)
	if err != nil {
		return state, err
	}
	return KnowledgeFixtureState{ObservedAt: time.Now().UTC(), Stand: f.Stand,
		Database: RegistrationFixtureDatabase, Actor: knowledgeFixtureActor, Scope: current}, nil
}

func knowledgeFixtureGuard(ctx context.Context, tx pgx.Tx) error {
	if _, err := tx.Exec(
		ctx,
		`LOCK TABLE core.knowledge_scopes, core.knowledge_permissions IN ACCESS SHARE MODE`,
	); err != nil {
		return err
	}
	var allowed bool
	err := tx.QueryRow(ctx, `SELECT count(*)=2 AND bool_and(pg_get_userbyid(c.relowner)='zns_app')
 FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace
 WHERE n.nspname='core' AND c.relkind='r' AND c.relname IN ('knowledge_scopes','knowledge_permissions')`).Scan(&allowed)
	if err != nil {
		return err
	}
	if !allowed {
		return errors.New("knowledge fixture ownership guard failed")
	}
	err = tx.QueryRow(ctx, `SELECT count(*)=3 FROM core.knowledge_scopes
 WHERE (scope='' AND event_id IS NULL) OR (scope IN ('sandbox-festival','sandbox-past') AND event_id=scope)`).Scan(&allowed)
	if err != nil {
		return err
	}
	if !allowed {
		return errors.New("knowledge fixture scope binding guard failed")
	}
	// Permit only ACL metadata, not private bodies, submissions or provenance.
	err = tx.QueryRow(ctx, `SELECT
 has_table_privilege(current_user,'core.knowledge_permissions','SELECT')
 AND has_table_privilege(current_user,'core.knowledge_permissions','INSERT')
 AND has_table_privilege(current_user,'core.knowledge_permissions','DELETE')
 AND has_table_privilege(current_user,'core.knowledge_scopes','SELECT')
 AND has_column_privilege(current_user,'core.knowledge_scopes','scope','UPDATE')
 AND NOT has_column_privilege(current_user,'core.knowledge_scopes','event_id','UPDATE')
 AND NOT has_table_privilege(current_user,'core.knowledge_scopes','INSERT,DELETE,TRUNCATE,REFERENCES,TRIGGER')
 AND NOT has_table_privilege(current_user,'core.knowledge_permissions','UPDATE,TRUNCATE,REFERENCES,TRIGGER')
 AND NOT EXISTS(SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace
 WHERE n.nspname='core' AND c.relkind='r'
 AND (c.relname LIKE 'knowledge_%' OR c.relname LIKE 'memory_%' OR c.relname LIKE 'assistant_source_%')
 AND c.relname NOT IN ('knowledge_scopes','knowledge_permissions')
 AND (has_any_column_privilege(current_user,c.oid,'SELECT,INSERT,UPDATE,REFERENCES')
 OR has_table_privilege(current_user,c.oid,'DELETE,TRUNCATE,TRIGGER')))`).Scan(&allowed)
	if err != nil {
		return err
	}
	if !allowed {
		return errors.New("knowledge fixture privilege guard failed")
	}
	return nil
}
