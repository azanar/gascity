package main

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/gastownhall/gascity/internal/beads"
	mysql "github.com/go-sql-driver/mysql"
)

var writeBdMetadataDirectFunc = writeBdMetadataDirect

func configureBdStoreMetadataFallback(store *beads.BdStore, scopeRoot string, env map[string]string) {
	if store == nil {
		return
	}
	scopeRoot = strings.TrimSpace(scopeRoot)
	if scopeRoot == "" {
		return
	}
	host := strings.TrimSpace(env["GC_DOLT_HOST"])
	port := strings.TrimSpace(env["GC_DOLT_PORT"])
	user := strings.TrimSpace(env["GC_DOLT_USER"])
	password := env["GC_DOLT_PASSWORD"]
	database := readDeferredManagedDoltDatabase(filepath.Join(scopeRoot, ".beads", "metadata.json"), "")
	if port == "" || database == "" {
		return
	}
	store.SetMetadataFallback(func(id string, kvs map[string]string) error {
		return writeBdMetadataDirectFunc(host, port, user, password, database, id, kvs)
	})
}

func writeBdMetadataDirect(host, port, user, password, database, id string, kvs map[string]string) error {
	if strings.TrimSpace(id) == "" || len(kvs) == 0 {
		return nil
	}
	db, err := openManagedDoltDatabaseWithPassword(host, port, user, password, database)
	if err != nil {
		return err
	}
	defer db.Close() //nolint:errcheck

	keys := make([]string, 0, len(kvs))
	for key := range kvs {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var query strings.Builder
	query.WriteString("UPDATE issues SET metadata = JSON_SET(COALESCE(metadata, JSON_OBJECT())")
	args := make([]any, 0, len(keys)*2+1)
	for _, key := range keys {
		query.WriteString(", ?, ?")
		args = append(args, metadataJSONPath(key), kvs[key])
	}
	query.WriteString(") WHERE id = ?")
	args = append(args, id)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	res, err := db.ExecContext(ctx, query.String(), args...)
	if err != nil {
		return err
	}
	if rows, err := res.RowsAffected(); err == nil && rows > 0 {
		return nil
	}

	var exists int
	switch err := db.QueryRowContext(ctx, "SELECT 1 FROM issues WHERE id = ?", id).Scan(&exists); err {
	case nil:
		return nil
	case sql.ErrNoRows:
		return beads.ErrNotFound
	default:
		return err
	}
}

func metadataJSONPath(key string) string {
	key = strings.ReplaceAll(key, `\`, `\\`)
	key = strings.ReplaceAll(key, `"`, `\"`)
	return `$."` + key + `"`
}

func openManagedDoltDatabaseWithPassword(host, port, user, password, database string) (*sql.DB, error) {
	host = managedDoltConnectHost(host)
	port = strings.TrimSpace(port)
	if port == "" {
		return nil, fmt.Errorf("missing port")
	}
	user = strings.TrimSpace(user)
	if user == "" {
		user = "root"
	}
	database = strings.TrimSpace(database)
	if database == "" {
		return nil, fmt.Errorf("missing database")
	}
	cfg := mysql.NewConfig()
	cfg.User = user
	cfg.Passwd = password
	cfg.Net = "tcp"
	cfg.Addr = host + ":" + port
	cfg.DBName = database
	cfg.Timeout = 5 * time.Second
	cfg.ReadTimeout = 5 * time.Second
	cfg.WriteTimeout = 5 * time.Second
	cfg.AllowNativePasswords = true
	return sql.Open("mysql", cfg.FormatDSN())
}
