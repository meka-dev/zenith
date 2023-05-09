package pgstore

import (
	"context"
	"fmt"
	"math/rand"
	"net/url"
	"os"
	"testing"
	"time"

	"zenith/store"

	"github.com/go-kit/log"
	pgx "github.com/jackc/pgx/v4"
)

func NewTestStore(t *testing.T) store.Store {
	t.Helper()

	rand.Seed(time.Now().UnixNano())

	ctx := context.Background()
	connStr := os.Getenv("PGCONNSTRING")
	if connStr == "" {
		t.Skipf("set PGCONNSTRING to run this test")
	}

	cfg, err := pgx.ParseConfig(connStr)
	if err != nil {
		t.Fatalf("parse connection string: %v", err)
	}

	cfg.Database = "postgres"
	conn, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("connect to database: %v", err)
	}

	dbName := fmt.Sprintf("zenith-test-%d", rand.Int())

	if _, err := conn.Exec(ctx, fmt.Sprintf(`drop database if exists %q`, dbName)); err != nil {
		t.Fatalf("init test DB: %v", err)
	}

	if _, err := conn.Exec(ctx, fmt.Sprintf(`create database %q`, dbName)); err != nil {
		t.Fatalf("init test DB: %v", err)
	}

	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("database %s left intact", dbName)
			return
		}

		if _, err := conn.Exec(ctx, `
			SELECT pg_terminate_backend(pg_stat_activity.pid)
			FROM pg_stat_activity
			WHERE datname = $1
		`, dbName); err != nil {
			t.Errorf("kill clients query: %v", err)
		}

		if _, err := conn.Exec(ctx, fmt.Sprintf(`drop database %q`, dbName)); err != nil {
			t.Errorf("drop test DB: %v", err)
		}

		if err := conn.Close(ctx); err != nil {
			t.Errorf("failed to close outer DB connection: %v", err)
		}
	})

	u, err := url.Parse(cfg.ConnString())
	if err != nil {
		t.Fatalf("re-parse test DB connection string: %v", err)
	}

	u.Path = dbName

	t.Logf("connection string %s", u.String())

	s, err := NewStore(ctx, u.String(), log.NewNopLogger())
	if err != nil {
		t.Fatalf("create test DB store: %v", err)
	}

	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Errorf("close test DB store: %v", err)
		}
	})

	return s
}
