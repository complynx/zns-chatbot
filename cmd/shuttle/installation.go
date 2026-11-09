package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/complynx/zns-chatbot/internal/db"
)

const initialTaskLimit = 8

func registerInstallationFlags(flags *flag.FlagSet) *db.InstallationOptions {
	o := &db.InstallationOptions{}
	flags.StringVar(&o.RuntimeRole, "install-runtime-role", "", "Grant runtime access after migrations; role must exist")
	flags.StringVar(&o.DefaultLanguage, "initial-language", "en", "Initial language for an unused database only")
	flags.IntVar(&o.MaxParallelTasks, "initial-parallel-tasks", initialTaskLimit,
		"Initial task limit for an unused database only")
	return o
}

func installationOptions(flags *flag.FlagSet, migrate bool, role, language string, parallel int) (
	db.InstallationOptions, error,
) {
	requested := false
	flags.Visit(func(f *flag.Flag) {
		if f.Name == "install-runtime-role" || f.Name == "initial-language" || f.Name == "initial-parallel-tasks" {
			requested = true
		}
	})
	if !requested {
		return db.InstallationOptions{}, nil
	}
	if !migrate {
		return db.InstallationOptions{}, errors.New("installation options require -migrate")
	}
	o := db.InstallationOptions{RuntimeRole: role, DefaultLanguage: language, MaxParallelTasks: parallel}
	if err := o.Validate(); err != nil {
		return db.InstallationOptions{}, err
	}
	return o, nil
}

func migrateAndInstall(ctx context.Context, store *db.Store, o db.InstallationOptions, output io.Writer) error {
	if err := store.CheckMigrationAccess(ctx, o.RuntimeRole); err != nil {
		return fmt.Errorf("migration preflight: %w", err)
	}
	if err := migrateDatabase(ctx, store); err != nil {
		return err
	}
	if o.RuntimeRole == "" {
		return nil
	}
	result, err := store.InstallRuntime(ctx, o)
	if err != nil {
		return fmt.Errorf("install runtime access and initial settings: %w", err)
	}
	return json.NewEncoder(output).Encode(result)
}
