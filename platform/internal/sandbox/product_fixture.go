package sandbox

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ApplyProductFixture creates opt-in synthetic scenarios. Repeating it never
// resets permissions, facts, dates or bookings changed during an acceptance run.
func ApplyProductFixture(ctx context.Context, db *pgxpool.Pool) error {
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(918431003)`); err != nil {
		return err
	}
	var count int
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM core.users WHERE
 (id='alice' AND telegram_id=101) OR (id='bob' AND telegram_id=202) OR (id='visitor' AND telegram_id=303)`).
		Scan(&count); err != nil {
		return err
	}
	const syntheticUsers = 3
	if count != syntheticUsers {
		return errors.New("product fixture requires the three original synthetic users")
	}
	if _, err = tx.Exec(ctx, `CREATE TABLE IF NOT EXISTS public.zns_sandbox_fixtures
 (name text PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		return err
	}
	if err = applyProductScenario(
		ctx,
		tx,
		"product-v1",
		fixtureRegistration,
		fixtureKnowledge,
		fixtureMassage,
	); err != nil {
		return err
	}
	if err = applyProductScenario(ctx, tx, "product-passport-v1", fixturePassportPair); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func applyProductScenario(ctx context.Context, tx pgx.Tx, name string, statements ...string) error {
	marker, err := tx.Exec(ctx, `INSERT INTO public.zns_sandbox_fixtures(name) VALUES($1) ON CONFLICT DO NOTHING`, name)
	if err != nil || marker.RowsAffected() == 0 {
		return err
	}
	for _, statement := range statements {
		if _, err = tx.Exec(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}

const fixturePassportPair = `
WITH created AS (
 INSERT INTO core.pass_events(id,finishes_at,titles,passport_required)
 VALUES('sandbox-passport-pair',now()+interval '7 days',
 '{"en":"Passport and partner practice","ru":"Паспорт и пара: учебное событие"}',true)
 ON CONFLICT DO NOTHING RETURNING id
), tiers AS (
 INSERT INTO core.pass_event_tiers(event_id,position,amount,price,starts_at)
 SELECT id,t.position,100,t.price,now()-interval '1 day' FROM created
 CROSS JOIN (VALUES(0,150),(1,200)) AS t(position,price)
 RETURNING event_id
)
INSERT INTO core.pass_payment_admins(event_id,owner) SELECT id,'bob' FROM created;`

const fixtureRegistration = `
INSERT INTO core.pass_events(id,finishes_at,titles) VALUES
 ('sandbox-festival',now()+interval '7 days','{"en":"Sandbox festival","ru":"Тестовый фестиваль"}'),
 ('sandbox-past',now()-interval '1 day','{"en":"Past festival","ru":"Прошлый фестиваль"}')
 ON CONFLICT DO NOTHING;
INSERT INTO core.pass_event_tiers(event_id,position,amount,price,starts_at)
 VALUES('sandbox-festival',0,100,150,now()-interval '1 day') ON CONFLICT DO NOTHING;
INSERT INTO core.pass_booking_admins(owner) VALUES('bob') ON CONFLICT DO NOTHING;
INSERT INTO core.pass_payment_admins(event_id,owner) VALUES('sandbox-festival','bob') ON CONFLICT DO NOTHING;`

const fixtureKnowledge = `
INSERT INTO core.knowledge_scopes(scope,event_id) VALUES
 ('sandbox-festival','sandbox-festival'),('sandbox-past','sandbox-past') ON CONFLICT DO NOTHING;
INSERT INTO core.knowledge_permissions(scope,actor,permission)
 SELECT scope,'bob',permission FROM core.knowledge_scopes CROSS JOIN
 (VALUES('curate'),('review')) AS permissions(permission)
 WHERE scope IN ('','sandbox-festival','sandbox-past') ON CONFLICT DO NOTHING;
INSERT INTO core.knowledge_facts(scope,topic,fact_key,body,version) VALUES
 ('','venue','accessibility','Ask staff for the step-free entrance. / Уточните у команды вход без ступеней.',1),
 ('sandbox-past','dress-code','main','Last year: red. / В прошлом году: красный.',1),
 ('sandbox-festival','dress-code','main','This event: blue. / На этом событии: синий.',1)
 ON CONFLICT DO NOTHING;`

const fixtureMassage = `
INSERT INTO core.massage_events(id) VALUES('sandbox-festival') ON CONFLICT DO NOTHING;
INSERT INTO core.massage_parties(id,event_id,starts_at,ends_at,tables,is_open)
 VALUES('sandbox-party','sandbox-festival',date_trunc('hour',now())+interval '1 hour',
 date_trunc('hour',now())+interval '9 hours',1,false) ON CONFLICT DO NOTHING;
INSERT INTO core.massage_specialists(event_id,owner,name,about,max_length)
 VALUES('sandbox-festival','bob','Борис',
 '{"en":"Synthetic massage specialist","ru":"Тестовый специалист по массажу"}',6) ON CONFLICT DO NOTHING;
INSERT INTO core.massage_work(event_id,specialist,starts_at,ends_at)
 SELECT event_id,'bob',starts_at,ends_at FROM core.massage_parties p WHERE p.id='sandbox-party'
 AND NOT EXISTS(SELECT 1 FROM core.massage_work w WHERE w.event_id=p.event_id AND w.specialist='bob');`
