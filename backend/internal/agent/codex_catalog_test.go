package agent

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"agentdeck/internal/store"
)

func TestCodexInternalLogsNeverBecomeProjectsAfterCheckpointRestart(t *testing.T) {
	for _, source := range []string{`"exec"`, `{"subagent":{"thread_spawn":{"parent_thread_id":"root"}}}`} {
		t.Run(source, func(t *testing.T) {
			n := setup(t)
			root := t.TempDir()
			path := filepath.Join(root, "rollout.jsonl")
			body := fmt.Sprintf(`{"type":"session_meta","payload":{"id":"internal","cwd":"/release/hash","source":%s}}`+"\n", source) + `{"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"Automated task"}]}}` + "\n"
			os.WriteFile(path, []byte(body), 0600)
			// Reproduce a previous release's already-consumed metadata checkpoint.
			n.Store.Append(store.Event{ID: "old", Session: "internal", Agent: "codex", Source: "log", Kind: "message", Role: "user", Text: "Old history"})
			n.Store.Put("checkpoints", digest(path), State{Session: "internal", Offset: int64(len(body))})
			f := &Follower{Normalizer: n, Roots: []Root{{"codex", root, "log"}}}
			f.Scan()
			ss, _ := n.Store.Sessions()
			if len(ss) != 0 {
				t.Fatal("internal log remained visible")
			}
			events, _ := n.Store.Events("internal", 0, 100)
			if len(events) != 1 {
				t.Fatal("history lost or internal task imported")
			}
			revision := n.Store.SessionCatalogRevision()
			f = &Follower{Normalizer: n, Roots: f.Roots}
			f.Scan()
			if n.Store.SessionCatalogRevision() != revision {
				t.Fatal("unchanged restart changed catalog")
			}
		})
	}
}

func TestCodexNativeCatalogRetiresOrphansAndPreservesUserChats(t *testing.T) {
	n := setup(t)
	root := t.TempDir()
	path := filepath.Join(root, "state_5.sqlite")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.Exec(`CREATE TABLE threads(id TEXT,source TEXT,archived INTEGER); INSERT INTO threads VALUES ('user','vscode',0),('cli','cli',0),('exec','exec',0),('sub','{"subagent":{}}',0),('archived','vscode',1)`); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"user", "cli", "exec", "sub", "archived", "orphan", "owned"} {
		s := store.Session{ID: id, Agent: "codex", Source: "log", Managed: id == "owned"}
		n.Store.Put("sessions", id, s)
	}
	f := &Follower{Normalizer: n, Index: filepath.Join(root, "session_index.jsonl")}
	f.Scan()
	ss, _ := n.Store.Sessions()
	want := map[string]bool{"user": true, "cli": true, "owned": true}
	if len(ss) != len(want) {
		t.Fatalf("visible count %d", len(ss))
	}
	for _, s := range ss {
		if !want[s.ID] {
			t.Fatal("internal session visible", s.ID)
		}
	}
	rev := n.Store.SessionCatalogRevision()
	f.codexChecked = time.Time{}
	f.Scan()
	if n.Store.SessionCatalogRevision() != rev {
		t.Fatal("unchanged catalog revision")
	}
	// Native catalog failures cannot wipe a working sidebar.
	db.Exec(`DROP TABLE threads`)
	f.codexChecked = time.Time{}
	f.codexStamp = ""
	f.Scan()
	ss, _ = n.Store.Sessions()
	if len(ss) != len(want) {
		t.Fatal("failed native query changed visibility")
	}
	// Restoring a formerly orphaned native user chat makes its history visible.
	db.Exec(`CREATE TABLE threads(id TEXT,source TEXT,archived INTEGER); INSERT INTO threads VALUES ('user','vscode',0),('cli','cli',0),('orphan','vscode',0)`)
	f.codexChecked = time.Time{}
	f.codexStamp = ""
	f.Scan()
	ss, _ = n.Store.Sessions()
	if len(ss) != 4 {
		t.Fatal("native chat did not reappear")
	}
}

func TestCodexNewInternalLogExcludedButUserInHashDirectoryKept(t *testing.T) {
	n := setup(t)
	root := t.TempDir()
	for id, source := range map[string]string{"exec": `"exec"`, "child": `{"subagent":{}}`, "user": `"vscode"`} {
		body := fmt.Sprintf(`{"type":"session_meta","payload":{"id":%q,"cwd":"/project/releases/1dabe5e968dbf9f9037b7bf756c68d83e97909c7d86f9d5dd0dc16637c29e7c0","source":%s}}`+"\n", id, source)
		body += `{"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"Fixture input"}]}}` + "\n"
		if err := os.WriteFile(filepath.Join(root, id+".jsonl"), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	f := &Follower{Normalizer: n, Roots: []Root{{"codex", root, "log"}}}
	f.Scan()
	ss, err := n.Store.Sessions()
	if err != nil || len(ss) != 1 || ss[0].ID != "user" || n.Store.LastSeq() != 1 {
		t.Fatal("source filtering failed or guessed visibility from project path", err)
	}
}
