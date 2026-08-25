package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "github.com/lib/pq"
)

// DB wraps the sql.DB pool and owns migrations.
type DB struct {
	*sql.DB
}

func Open(ctx context.Context, dsn string) (*DB, error) {
	pool, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, err
	}
	pool.SetMaxOpenConns(8)
	pool.SetMaxIdleConns(4)
	pool.SetConnMaxLifetime(30 * time.Minute)

	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := pool.PingContext(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping: %w", err)
	}
	return &DB{pool}, nil
}

func (d *DB) Close() { d.DB.Close() }
