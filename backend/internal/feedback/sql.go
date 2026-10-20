package feedback

import (
	"context"
	"database/sql"
	"errors"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/stdlib"
)

const table = "agentdeck_feedback_v1"

type sqlRepository struct {
	db     *sql.DB
	driver string
}

func localHost(host string) bool {
	ip := net.ParseIP(host)
	return host == "localhost" || ip != nil && ip.IsLoopback() || strings.HasPrefix(host, "/")
}
func openSQL(ctx context.Context, c Config) (repository, error) {
	var db *sql.DB
	if c.Driver == "mysql" {
		cfg, e := mysql.ParseDSN(c.DSN)
		if e != nil || cfg.DBName == "" {
			return nil, ErrInvalid
		}
		host, _, e := net.SplitHostPort(cfg.Addr)
		local := cfg.Net == "unix" || e == nil && localHost(host)
		if cfg.MultiStatements || cfg.AllowAllFiles || cfg.TLSConfig == "skip-verify" || cfg.TLSConfig == "preferred" || !local && cfg.TLSConfig != "true" {
			return nil, ErrInvalid
		}
		cfg.Timeout = 5 * time.Second
		cfg.ReadTimeout = 10 * time.Second
		cfg.WriteTimeout = 10 * time.Second
		connector, e := mysql.NewConnector(cfg)
		if e != nil {
			return nil, e
		}
		db = sql.OpenDB(connector)
	} else {
		cfg, e := pgx.ParseConfig(c.DSN)
		if e != nil || cfg.Database == "" {
			return nil, ErrInvalid
		}
		if !localHost(cfg.Host) {
			if cfg.TLSConfig == nil || cfg.TLSConfig.InsecureSkipVerify {
				return nil, ErrInvalid
			}
		}
		for _, fallback := range cfg.Fallbacks {
			if !localHost(fallback.Host) && (fallback.TLSConfig == nil || fallback.TLSConfig.InsecureSkipVerify) {
				return nil, ErrInvalid
			}
		}
		cfg.ConnectTimeout = 5 * time.Second
		db = stdlib.OpenDB(*cfg)
	}
	db.SetMaxOpenConns(8)
	db.SetMaxIdleConns(4)
	db.SetConnMaxLifetime(30 * time.Minute)
	r := &sqlRepository{db: db, driver: c.Driver}
	if e := db.PingContext(ctx); e != nil {
		db.Close()
		return nil, e
	}
	ddl := `CREATE TABLE IF NOT EXISTS ` + table + ` (scope CHAR(64) NOT NULL, id VARCHAR(38) NOT NULL, fingerprint CHAR(64) NOT NULL, created BIGINT NOT NULL, expires BIGINT NOT NULL, revision BIGINT NOT NULL, payload MEDIUMBLOB NOT NULL, PRIMARY KEY (scope,id), INDEX feedback_scope_created (scope,created,id), INDEX feedback_expires (expires)) ENGINE=InnoDB`
	if c.Driver == "pg" {
		ddl = `CREATE TABLE IF NOT EXISTS ` + table + ` (scope CHAR(64) NOT NULL, id VARCHAR(38) NOT NULL, fingerprint CHAR(64) NOT NULL, created BIGINT NOT NULL, expires BIGINT NOT NULL, revision BIGINT NOT NULL, payload BYTEA NOT NULL, PRIMARY KEY (scope,id))`
	}
	if _, e := db.ExecContext(ctx, ddl); e != nil {
		db.Close()
		return nil, e
	}
	if c.Driver == "pg" {
		for _, q := range []string{`CREATE INDEX IF NOT EXISTS agentdeck_feedback_scope_created_v1 ON ` + table + ` (scope,created,id)`, `CREATE INDEX IF NOT EXISTS agentdeck_feedback_expires_v1 ON ` + table + ` (expires)`} {
			if _, e := db.ExecContext(ctx, q); e != nil {
				db.Close()
				return nil, e
			}
		}
	}
	return r, nil
}
func (r *sqlRepository) query(q string) string {
	if r.driver != "pg" {
		return q
	}
	i := 0
	var b strings.Builder
	for _, v := range q {
		if v == '?' {
			i++
			b.WriteString("$" + strconv.Itoa(i))
		} else {
			b.WriteRune(v)
		}
	}
	return b.String()
}
func (r *sqlRepository) Insert(ctx context.Context, v row) (bool, error) {
	_, e := r.db.ExecContext(ctx, r.query(`INSERT INTO `+table+` (scope,id,fingerprint,created,expires,revision,payload) VALUES (?,?,?,?,?,?,?)`), v.Scope, v.ID, v.Fingerprint, v.Created, v.Expires, v.Revision, v.Payload)
	var my *mysql.MySQLError
	var pg *pgconn.PgError
	if errors.As(e, &my) && my.Number == 1062 || errors.As(e, &pg) && pg.Code == "23505" {
		return false, nil
	}
	return e == nil, e
}

type scanner interface{ Scan(...any) error }

func scanRow(s scanner) (row, error) {
	var r row
	e := s.Scan(&r.Scope, &r.ID, &r.Fingerprint, &r.Created, &r.Expires, &r.Revision, &r.Payload)
	if errors.Is(e, sql.ErrNoRows) {
		e = ErrNotFound
	}
	return r, e
}
func (r *sqlRepository) Get(ctx context.Context, scope, id string, now int64) (row, error) {
	return scanRow(r.db.QueryRowContext(ctx, r.query(`SELECT scope,id,fingerprint,created,expires,revision,payload FROM `+table+` WHERE scope=? AND id=? AND expires>?`), scope, id, now))
}
func (r *sqlRepository) List(ctx context.Context, scope string, c cursor, limit int, now int64) ([]row, error) {
	q := `SELECT scope,id,fingerprint,created,expires,revision,payload FROM ` + table + ` WHERE scope=? AND expires>?`
	args := []any{scope, now}
	if c.ID != "" {
		q += ` AND (created<? OR (created=? AND id<?))`
		args = append(args, c.Created, c.Created, c.ID)
	}
	q += ` ORDER BY created DESC,id DESC LIMIT ?`
	args = append(args, limit)
	rows, e := r.db.QueryContext(ctx, r.query(q), args...)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	var out []row
	for rows.Next() {
		v, e := scanRow(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (r *sqlRepository) Replace(ctx context.Context, v row, version int64) (bool, error) {
	result, e := r.db.ExecContext(ctx, r.query(`UPDATE `+table+` SET payload=?,revision=? WHERE scope=? AND id=? AND revision=? AND expires>?`), v.Payload, v.Revision, v.Scope, v.ID, version, time.Now().UnixMilli())
	if e != nil {
		return false, e
	}
	n, e := result.RowsAffected()
	return n == 1, e
}
func (r *sqlRepository) Delete(ctx context.Context, scope, id string) error {
	_, e := r.db.ExecContext(ctx, r.query(`DELETE FROM `+table+` WHERE scope=? AND id=?`), scope, id)
	return e
}
func (r *sqlRepository) Prune(ctx context.Context, now int64) error {
	_, e := r.db.ExecContext(ctx, r.query(`DELETE FROM `+table+` WHERE expires<=?`), now)
	return e
}
func (r *sqlRepository) Close() error { return r.db.Close() }
