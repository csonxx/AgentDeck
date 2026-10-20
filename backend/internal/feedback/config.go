package feedback

import (
	"context"
	"encoding/base64"
	"errors"
	"regexp"
	"strings"
	"time"
)

type Config struct {
	Driver    string
	DSN       string
	Database  string
	Key       []byte
	Retention time.Duration
}

// Secrets stay in the environment and never enter errors or diagnostics.
func FromEnv(get func(string) string) (Config, error) {
	c := Config{Driver: strings.ToLower(strings.TrimSpace(get("AGENTDECK_FEEDBACK_DRIVER"))), DSN: get("AGENTDECK_FEEDBACK_DSN"), Database: get("AGENTDECK_FEEDBACK_DATABASE"), Retention: 30 * 24 * time.Hour}
	switch c.Driver {
	case "", "off":
		c.Driver = "off"
	case "mongo", "mongodb":
		c.Driver = "mongo"
	case "mysql":
	case "pg", "postgres", "postgresql":
		c.Driver = "pg"
	default:
		return c, errors.New("AGENTDECK_FEEDBACK_DRIVER must be off, mongo, mysql or pg")
	}
	key := get("AGENTDECK_FEEDBACK_KEY")
	if c.Driver == "off" {
		if c.DSN != "" || key != "" || c.Database != "" || get("AGENTDECK_FEEDBACK_RETENTION") != "" {
			return c, errors.New("feedback settings require an explicit feedback driver")
		}
		return c, nil
	}
	var e error
	c.Key, e = base64.StdEncoding.DecodeString(key)
	if e != nil || len(c.Key) != 32 {
		return c, errors.New("AGENTDECK_FEEDBACK_KEY must be a base64-encoded 32-byte key")
	}
	if value := get("AGENTDECK_FEEDBACK_RETENTION"); value != "" {
		c.Retention, e = time.ParseDuration(value)
		if e != nil {
			return c, errors.New("invalid AGENTDECK_FEEDBACK_RETENTION")
		}
	}
	if c.DSN == "" || c.Retention < 24*time.Hour || c.Retention > 365*24*time.Hour || c.Driver == "mongo" && !databaseName.MatchString(c.Database) {
		return c, errors.New("feedback requires a DSN, retention between 24h and 8760h, and a Mongo database name")
	}
	return c, nil
}

var databaseName = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_-]{0,62}$`)

func Open(ctx context.Context, c Config) (*Service, error) {
	if c.Driver == "off" {
		return nil, nil
	}
	if len(c.Key) != 32 || c.DSN == "" || c.Retention < 24*time.Hour || c.Retention > 365*24*time.Hour {
		return nil, ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	var r repository
	var e error
	switch c.Driver {
	case "mysql", "pg":
		r, e = openSQL(ctx, c)
	case "mongo":
		r, e = openMongo(ctx, c)
	default:
		return nil, ErrInvalid
	}
	if e != nil {
		return nil, errors.New("feedback storage initialization failed; check connectivity, TLS, database and schema permissions")
	}
	return &Service{repo: r, key: append([]byte(nil), c.Key...), retention: c.Retention, now: time.Now}, nil
}
