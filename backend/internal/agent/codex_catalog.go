package agent

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"time"
)

func codexInternalSource(source any) bool {
	if text, ok := source.(string); ok {
		if text == "exec" || text == "subagent" {
			return true
		}
		var decoded map[string]any
		if json.Unmarshal([]byte(text), &decoded) == nil {
			source = decoded
		}
	}
	_, internal := obj(source)["subagent"]
	return internal
}

// Read only native identity/source metadata, never prompts or chat bodies.
// A missing or unreadable native catalog must not empty the phone's sidebar.
func (f *Follower) refreshCodexCatalog() {
	if f.Index == "" || time.Since(f.codexChecked) < 5*time.Second {
		return
	}
	f.codexChecked = time.Now()
	path := filepath.Join(filepath.Dir(f.Index), "state_5.sqlite")
	info, err := os.Stat(path)
	if err != nil {
		return
	}
	stamp := info.ModTime().String()
	if wal, err := os.Stat(path + "-wal"); err == nil {
		stamp += wal.ModTime().String()
	}
	if stamp == f.codexStamp {
		return
	}
	u := url.URL{Scheme: "file", Path: path, RawQuery: "mode=ro&_pragma=busy_timeout(1000)"}
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	rows, err := db.QueryContext(ctx, `SELECT id, source, archived FROM threads`)
	if err != nil {
		return
	}
	defer rows.Close()
	visible := map[string]bool{}
	for rows.Next() {
		var id, source string
		var archived bool
		if rows.Scan(&id, &source, &archived) != nil {
			return
		}
		visible[id] = !archived && !codexInternalSource(source)
	}
	if rows.Err() != nil || len(visible) == 0 {
		return
	}
	if f.Normalizer.Store.SyncCodexVisibility(visible, true) == nil {
		f.codexStamp = stamp
	}
}
