// Package pgtest gives tests a throwaway PostgreSQL database with every
// migration applied. The behaviour under test is mostly SQL, so these tests
// run against a real server: set TEST_DATABASE_URL to any database on a
// server you can create databases on (postgres://user:pass@localhost:5432/postgres).
// Without it the tests are skipped.
package pgtest

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Pool creates a temporary database, applies the migrations (like the server at
// boot: a failing one is logged, not fatal) and drops the database after the test.
func Pool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	admin := os.Getenv("TEST_DATABASE_URL")
	if admin == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	ap, err := pgxpool.New(ctx, admin)
	if err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("mdm_test_%d", time.Now().UnixNano())
	if _, err := ap.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		ap.Close()
		t.Fatal(err)
	}
	cfg, err := pgxpool.ParseConfig(admin)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.Database = name
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Close()
		ap.Exec(ctx, "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
		ap.Close()
	})
	Migrate(t, pool)
	return pool
}

// Migrate applies every migration in order, the way the server does at each start:
// a failing one is logged, not fatal. It can be called again on a database that
// already has data — the server runs the migrations again at every restart, and
// they must leave that data alone.
func Migrate(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	_, here, _, _ := runtime.Caller(0)
	files, _ := filepath.Glob(filepath.Join(filepath.Dir(here), "../../db/migrations/*.up.sql"))
	sort.Strings(files)
	for _, f := range files {
		sqlText, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(context.Background(), string(sqlText)); err != nil {
			t.Logf("migration %s: %v", filepath.Base(f), err)
		}
	}
}
