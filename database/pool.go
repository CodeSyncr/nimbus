package database

import (
	"strings"
	"time"

	"github.com/CodeSyncr/nimbus/lucid"
)

// PoolConfig tunes the underlying database/sql connection pool. Zero values
// for each field mean "do not change" (keep Go or driver defaults).
type PoolConfig struct {
	// MaxOpenConns caps open connections (0 = leave default, often unlimited).
	MaxOpenConns int
	// MaxIdleConns caps idle connections in the pool (0 = leave default).
	MaxIdleConns int
	// ConnMaxLifetime is the maximum amount of time a connection may be reused
	// (0 = leave default). Set this in production behind load balancers or
	// managed databases so connections are recycled before the server drops them.
	ConnMaxLifetime time.Duration
	// ConnMaxIdleTime is how long idle connections stay in the pool (0 = leave default).
	ConnMaxIdleTime time.Duration
}

// ApplyPool applies non-zero pool settings to the given GORM handle.
func ApplyPool(db *lucid.DB, p PoolConfig) error {
	if db == nil {
		return nil
	}
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	if p.MaxOpenConns > 0 {
		sqlDB.SetMaxOpenConns(p.MaxOpenConns)
	}
	if p.MaxIdleConns > 0 {
		sqlDB.SetMaxIdleConns(p.MaxIdleConns)
	}
	if p.ConnMaxLifetime > 0 {
		sqlDB.SetConnMaxLifetime(p.ConnMaxLifetime)
	}
	if p.ConnMaxIdleTime > 0 {
		sqlDB.SetConnMaxIdleTime(p.ConnMaxIdleTime)
	}
	return nil
}

// PoolConfigFromFields builds a PoolConfig from typical config/env strings.
// Duration values use time.ParseDuration (e.g. "5m", "90s", "1h"). Invalid
// strings are ignored.
func PoolConfigFromFields(maxOpen, maxIdle int, connMaxLifetime, connMaxIdleTime string) PoolConfig {
	p := PoolConfig{
		MaxOpenConns: maxOpen,
		MaxIdleConns: maxIdle,
	}
	if s := strings.TrimSpace(connMaxLifetime); s != "" {
		if d, err := time.ParseDuration(s); err == nil {
			p.ConnMaxLifetime = d
		}
	}
	if s := strings.TrimSpace(connMaxIdleTime); s != "" {
		if d, err := time.ParseDuration(s); err == nil {
			p.ConnMaxIdleTime = d
		}
	}
	return p
}
