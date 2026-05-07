package main

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	mysql "github.com/go-sql-driver/mysql"
)

var managedDoltApplySafeGlobalsFn = applyManagedDoltSafeGlobals

var managedDoltSafeGlobalStatements = []string{
	"SET GLOBAL dolt_stats_enabled = 0",
	"SET GLOBAL dolt_stats_paused = 1",
	"SET GLOBAL dolt_stats_gc_enabled = 0",
	"SET GLOBAL dolt_stats_job_interval = 3600000",
}

func applyManagedDoltSafeGlobals(cityPath, host, port, user string) error {
	if err := ensureManagedDoltPersistedGlobals(cityPath); err != nil {
		return err
	}
	db, err := managedDoltOpenServer(host, port, user)
	if err != nil {
		return err
	}
	defer db.Close() //nolint:errcheck

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		return err
	}
	for _, stmt := range managedDoltSafeGlobalStatements {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("%s: %w", stmt, err)
		}
	}
	return nil
}

func managedDoltOpenServer(host, port, user string) (*sql.DB, error) {
	host = managedDoltConnectHost(host)
	port = strings.TrimSpace(port)
	if port == "" {
		return nil, fmt.Errorf("missing port")
	}
	user = strings.TrimSpace(user)
	if user == "" {
		user = "root"
	}
	cfg := mysql.NewConfig()
	cfg.User = user
	cfg.Passwd = managedDoltPassword()
	cfg.Net = "tcp"
	cfg.Addr = host + ":" + port
	cfg.DBName = "information_schema"
	cfg.Timeout = 5 * time.Second
	cfg.ReadTimeout = 5 * time.Second
	cfg.WriteTimeout = 5 * time.Second
	cfg.AllowNativePasswords = true
	return sql.Open("mysql", cfg.FormatDSN())
}
