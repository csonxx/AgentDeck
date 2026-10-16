package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"agentdeck/internal/store"
)

// Decode only presentation metadata. Desktop files also contain settings,
// credentials and prompt snapshots; none of those fields enter our catalog.
type claudeDesktopRecord struct {
	SessionID string `json:"sessionId"`
	CLI       string `json:"cliSessionId"`
	CWD       string `json:"cwd"`
	Origin    string `json:"originCwd"`
	Title     string `json:"title"`
	Archived  bool   `json:"isArchived"`
	Created   int64  `json:"createdAt"`
	Activity  int64  `json:"lastActivityAt"`
}

type claudeCatalogFile struct {
	stamp  string
	record claudeDesktopRecord
}
type claudeCatalog struct {
	statusStamps  map[string]string
	runningTails  map[string]string
	orphanSeen    map[string]string
	processSeen   map[string]string
	processOwners map[string]claudeProcessRecord
	ready         bool
	checked       time.Time
	version       string
	cache         map[string]claudeCatalogFile
	entries       map[string]store.Session
	paths         map[string]string
	roots         map[string]Root
}

var claudeNativeID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,160}$`)

func (f *Follower) refreshClaudeCatalog() {
	if f.ClaudeIndex == "" {
		return
	}
	if f.claude == nil {
		f.claude = &claudeCatalog{cache: map[string]claudeCatalogFile{}}
	}
	c := f.claude
	if time.Since(c.checked) < 2*time.Second {
		return
	}
	c.checked = time.Now()
	if err := f.loadClaudeCatalog(); err != nil {
		log.Print("Claude desktop catalog refresh failed; retaining previous catalog")
	}
}

func (f *Follower) loadClaudeCatalog() error {
	c := f.claude
	if st, err := os.Stat(f.ClaudeIndex); err != nil || !st.IsDir() {
		return errors.New("Claude desktop catalog unavailable")
	}
	entries := map[string]store.Session{}
	cache := map[string]claudeCatalogFile{}
	err := filepath.WalkDir(f.ClaudeIndex, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasPrefix(d.Name(), "local_") || !strings.HasSuffix(d.Name(), ".json") {
			return nil
		}
		info, err := d.Info()
		if err != nil || !info.Mode().IsRegular() || info.Size() > 8<<20 {
			return errors.New("invalid Claude catalog file")
		}
		stamp := fmt.Sprint(info.Size(), "/", info.ModTime().UnixNano())
		v, exists := c.cache[path]
		if !exists || v.stamp != stamp {
			file, err := os.Open(path)
			if err != nil {
				return err
			}
			decoder := json.NewDecoder(io.LimitReader(file, (8<<20)+1))
			v = claudeCatalogFile{stamp: stamp}
			err = decoder.Decode(&v.record)
			if err == nil && decoder.Decode(new(any)) != io.EOF {
				err = errors.New("invalid Claude catalog JSON")
			}
			file.Close()
			if err != nil {
				return err
			}
		}
		meta := v.record
		if !claudeNativeID.MatchString(meta.SessionID) || meta.SessionID+".json" != d.Name() || (meta.CLI != "" && !claudeNativeID.MatchString(meta.CLI)) {
			return errors.New("invalid Claude session identity")
		}
		cache[path] = v
		id := meta.CLI
		if id == "" {
			id = "claude-desktop/" + meta.SessionID
		}
		project := meta.Origin
		if project == "" {
			project = meta.CWD
		}
		if !filepath.IsAbs(project) {
			return errors.New("invalid Claude project")
		}
		activity := meta.Activity
		if activity <= 0 {
			activity = meta.Created
		}
		title := strings.TrimSpace(Redact(meta.Title))
		if title == "" {
			title = "新会话"
		}
		entry := store.Session{ID: id, Agent: "claude", Source: "claude-desktop", Title: title,
			Project: filepath.Clean(project), Updated: time.UnixMilli(activity).UTC().Format(time.RFC3339Nano), Status: "idle", CatalogHidden: meta.Archived}
		if prior, exists := entries[id]; !exists || prior.Updated < entry.Updated {
			entries[id] = entry
		}
		return nil
	})
	if err != nil {
		return err
	}
	// Only top-level native chats may supply history. Subagent files cannot
	// become sidebar entries or be substituted for a parent session's history.
	paths, roots := map[string]string{}, map[string]Root{}
	for _, root := range f.Roots {
		if root.Agent != "claude" {
			continue
		}
		files, err := filepath.Glob(filepath.Join(root.Path, "*", "*.jsonl"))
		if err != nil {
			return err
		}
		for _, path := range files {
			id := strings.TrimSuffix(filepath.Base(path), ".jsonl")
			if st, err := os.Lstat(path); err == nil && st.Mode().IsRegular() {
				paths[id], roots[id] = path, root
			}
		}
	}
	raw, _ := json.Marshal(entries)
	version := digest(string(raw))
	if version != c.version {
		list := make([]store.Session, 0, len(entries))
		for _, entry := range entries {
			list = append(list, entry)
		}
		if err := f.Normalizer.Store.SyncClaudeCatalog(list); err != nil {
			return err
		}
	}
	c.cache, c.entries, c.paths, c.roots = cache, entries, paths, roots
	c.version, c.ready = version, true
	return nil
}

// LoadClaudeHistory runs under the same lock as incremental following. It
// imports only the selected chat's bounded tail, retaining exact checkpoints.
func (f *Follower) LoadClaudeHistory(id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.init()
	f.refreshClaudeCatalog()
	if f.claude == nil || !f.claude.ready {
		return nil
	}
	entry, exists := f.claude.entries[id]
	if !exists || entry.CatalogHidden {
		return errors.New("此 Claude 会话已从桌面目录移除或归档")
	}
	path := f.claude.paths[id]
	if path == "" {
		return errors.New("此会话保留在 Claude 桌面目录中，但本机没有对应的日志，暂时无法读取历史或从手机续聊")
	}
	f.requested[path] = true
	info, err := os.Stat(path)
	if err != nil {
		return errors.New("Claude 本机会话日志暂时无法读取")
	}
	target := info.Size()
	for {
		before := int64(-1)
		if state := f.states[path]; state != nil {
			before = state.Offset
		}
		if err := f.read(path, f.claude.roots[id]); err != nil {
			return errors.New("Claude 本机会话日志暂时无法读取")
		}
		state := f.states[path]
		if state == nil || state.Offset >= target || state.Offset == before {
			return nil
		}
	}
}
