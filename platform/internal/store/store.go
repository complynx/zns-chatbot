package store

import (
	"context"
	"crypto/sha256"
	"embed"
	"errors"
	"fmt"

	"github.com/complynx/zns-chatbot/platform/internal/orders"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrations embed.FS

func Open(ctx context.Context, url string, tracers ...pgx.QueryTracer) (*pgxpool.Pool, error) {
	return OpenNamed(ctx, url, "", tracers...)
}

// OpenNamed sets connection identity before the pool opens its first connection.
func OpenNamed(ctx context.Context, url, applicationName string, tracers ...pgx.QueryTracer) (*pgxpool.Pool, error) {
	config, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, errors.New("invalid database configuration")
	}
	if applicationName != "" {
		config.ConnConfig.RuntimeParams["application_name"] = applicationName
	}
	if len(tracers) > 1 {
		return nil, errors.New("only one database tracer is supported")
	}
	if len(tracers) == 1 {
		config.ConnConfig.Tracer = tracers[0]
	}
	p, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, errors.New("database pool initialization failed")
	}
	if err = p.Ping(ctx); err != nil {
		p.Close()
		return nil, errors.New("database connection failed")
	}
	return p, nil
}

// Migrate applies ordered forward changes under a database-wide transaction lock.
// A failed change rolls back the whole run. Applied SQL files are immutable.
func Migrate(ctx context.Context, p *pgxpool.Pool) error {
	tx, err := p.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }() // Rollback is cleanup after commit or a reported error.
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(918431002)`); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `CREATE TABLE IF NOT EXISTS public.zns_schema_migrations
	(name text PRIMARY KEY, checksum text NOT NULL, applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		return err
	}
	entries, err := migrations.ReadDir("migrations")
	if err != nil {
		return err
	}
	for _, entry := range entries {
		body, readError := migrations.ReadFile("migrations/" + entry.Name())
		if readError != nil {
			return readError
		}
		checksum := fmt.Sprintf("%x", sha256.Sum256(body))
		var previous string
		err = tx.QueryRow(ctx, `SELECT checksum FROM public.zns_schema_migrations WHERE name=$1`, entry.Name()).
			Scan(&previous)
		if err == nil {
			if previous != checksum {
				return fmt.Errorf("migration %s checksum mismatch", entry.Name())
			}
			continue
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if _, err = tx.Exec(ctx, string(body)); err != nil {
			return fmt.Errorf("migration %s: %w", entry.Name(), err)
		}
		if _, err = tx.Exec(
			ctx,
			`INSERT INTO public.zns_schema_migrations(name,checksum) VALUES($1,$2)`,
			entry.Name(),
			checksum,
		); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func Seed(ctx context.Context, p *pgxpool.Pool) error {
	_, err := p.Exec(ctx, `INSERT INTO core.users(id,telegram_id,name,can_book,language) VALUES
 ('alice',101,'Алиса',true,'ru'),('bob',202,'Борис',true,'ru'),('visitor',303,'Гость',false,'ru') ON CONFLICT DO NOTHING;
 INSERT INTO core.slots(id,title,capacity,price,currency,starts_at) VALUES
 ('massage-1','Массаж · 20 минут',1,30,'BYN','2030-10-02 18:00:00+00'),
 ('shuttle-1','Трансфер · до площадки',2,20,'BYN','2030-10-02 17:00:00+00') ON CONFLICT DO NOTHING;`)
	if err != nil {
		return err
	}
	if err = orders.Seed(ctx, p); err != nil {
		return err
	}
	_, err = p.Exec(ctx, `INSERT INTO core.order_admins(event_id,owner,country,region)
		VALUES('sandbox-festival','bob','be','Тестовый Минск') ON CONFLICT DO NOTHING`)
	return err
}
