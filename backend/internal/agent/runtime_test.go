package agent

import (
	"agentdeck/internal/store"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestProjectScope(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	_ = os.Mkdir(filepath.Join(root, "project"), 0700)
	_ = os.Symlink(outside, filepath.Join(root, "escape"))
	db, e := store.New(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	m := &Manager{Store: db, Roots: []string{root}}
	if _, e = m.allowed(filepath.Join(root, "escape")); e == nil {
		t.Fatal("symlink escaped allowlist")
	}
	if _, e = m.allowed(outside); e == nil {
		t.Fatal("outside accepted")
	}
	if len(m.Projects()) != 2 {
		t.Fatal("project list includes escape")
	}
	if _, e = m.Create("codex", "invalid", ""); e == nil {
		t.Fatal("unknown project accepted")
	}
}
func TestRealRuntime(t *testing.T) {
	path := os.Getenv("AGENTDECK_REAL_PROJECT")
	if path == "" {
		t.Skip("explicit real CLI acceptance only")
	}
	db, e := store.New(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m := &Manager{Store: db, Normalizer: &Normalizer{Store: db}, Roots: []string{path}, Context: ctx}
	defer m.Close()
	for _, name := range []string{"codex", "claude"} {
		t.Run(name, func(t *testing.T) {
			s, e := m.Create(name, m.Projects()[0].ID, "")
			if e != nil {
				t.Fatal(e)
			}
			if e = m.Send(s.ID, "Do not use tools or change files. Remember the word AGENTDECK_READY. Reply with only AGENTDECK_READY.", UUID()); e != nil {
				t.Fatal(e)
			}
			waitReply := func() {
				deadline := time.Now().Add(150 * time.Second)
				for time.Now().Before(deadline) {
					ss, _ := db.Sessions()
					status := ""
					for _, v := range ss {
						if v.ID == s.ID {
							status = v.Status
						}
					}
					evs, _ := db.Events(s.ID, 0, 100)
					found := false
					for _, ev := range evs {
						if ev.Role == "assistant" {
							found = strings.Contains(ev.Text, "AGENTDECK_READY")
						}
					}
					if status == "failed" {
						t.Fatal("real provider failed")
					}
					if status == "completed" && found {
						return
					}
					time.Sleep(100 * time.Millisecond)
				}
				t.Fatal("real reply timed out")
			}
			waitReply()
			if e = m.Send(s.ID, "Without tools, what word did I ask you to remember? Reply with only that word.", UUID()); e != nil {
				t.Fatal(e)
			}
			waitReply()
			fork, e := m.Create(name, m.Projects()[0].ID, s.ID)
			if e != nil {
				t.Fatal(e)
			}
			s = fork
			if e = m.Send(s.ID, "Without tools, recall the word I asked you to remember. Reply with only that word.", UUID()); e != nil {
				t.Fatal(e)
			}
			waitReply()
			m.mu.Lock()
			run := m.runs[s.ID]
			delete(m.runs, s.ID)
			m.mu.Unlock()
			run.close()
			<-run.done
			if e = m.Send(s.ID, "After resuming, recall the same word. Use no tools and reply with only that word.", UUID()); e != nil {
				t.Fatal(e)
			}
			waitReply()
		})
	}
}

func TestRuntimeApprovalAndStop(t *testing.T) {
	fixture, e := os.ReadFile("../../../scripts/fixture_runtime.py")
	if e != nil {
		t.Fatal(e)
	}
	bin := t.TempDir()
	for _, a := range []string{"codex", "claude"} {
		if e = os.WriteFile(filepath.Join(bin, a), fixture, 0700); e != nil {
			t.Fatal(e)
		}
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	root := t.TempDir()
	db, e := store.New(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	m := &Manager{Store: db, Normalizer: &Normalizer{Store: db}, Roots: []string{root}}
	defer m.Close()
	for _, a := range []string{"codex", "claude"} {
		t.Run(a, func(t *testing.T) {
			ss, e := m.Create(a, m.Projects()[0].ID, "")
			if e != nil {
				t.Fatal(e)
			}
			wait := func(kind, text string) store.Event {
				deadline := time.Now().Add(5 * time.Second)
				for time.Now().Before(deadline) {
					if kind == "message" {
						run, _ := m.get(ss.ID)
						run.mu.Lock()
						busy := run.busy
						run.mu.Unlock()
						if busy {
							time.Sleep(20 * time.Millisecond)
							continue
						}
					}
					events, _ := db.Events(ss.ID, 0, 100)
					for _, ev := range events {
						if ev.Kind == kind && (ev.Text == text || text == "" && ev.Status == "pending") {
							return ev
						}
					}
					time.Sleep(20 * time.Millisecond)
				}
				t.Fatal("missing protocol event")
				return store.Event{}
			}
			if e = m.Send(ss.ID, "approval", UUID()); e != nil {
				t.Fatal(e)
			}
			ev := wait("approval", "")
			if e = m.Approve(ss.ID, "wrong", "accept", ""); e == nil {
				t.Fatal("foreign approval accepted")
			}
			if e = m.Approve(ss.ID, ev.Item, "decline", ""); e != nil {
				t.Fatal(e)
			}
			wait("message", "DECLINED")
			if e = m.Approve(ss.ID, ev.Item, "accept", ""); e == nil {
				t.Fatal("repeated approval accepted")
			}
			if e = m.Send(ss.ID, "pause", UUID()); e != nil {
				t.Fatal(e)
			}
			if e = m.Send(ss.ID, "duplicate active turn", UUID()); e == nil && a != "claude" {
				t.Fatal("concurrent Codex turn accepted")
			} else if e != nil && a == "claude" {
				t.Fatal("native streaming append rejected", e)
			}
			if e = m.Interrupt(ss.ID); e != nil {
				t.Fatal(e)
			}
			wait("message", "STOPPED")
		})
	}
}

func TestRuntimeNativePermissionInheritanceAcrossCreateForkAndResume(t *testing.T) {
	fixture, err := os.ReadFile("../../../scripts/fixture_runtime.py")
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	for _, name := range []string{"codex", "claude"} {
		if err := os.WriteFile(filepath.Join(bin, name), fixture, 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("AGENTDECK_FIXTURE_NATIVE_PERMISSIONS", "1")
	for _, name := range []string{"codex", "claude"} {
		t.Run(name, func(t *testing.T) {
			m := connectorManager(t)
			ss, err := m.Create(name, m.Projects()[0].ID, "")
			if err != nil {
				t.Fatal(err)
			}
			if err := m.Send(ss.ID, "synthetic inheritance probe", UUID()); err != nil {
				t.Fatal(err)
			}
			awaitConnector(t, m, ss.ID, "status", "", "completed")
			if name == "codex" {
				ss, err = m.Create(name, m.Projects()[0].ID, ss.ID)
				if err != nil {
					t.Fatal(err)
				}
			}
			m.mu.Lock()
			r := m.runs[ss.ID]
			delete(m.runs, ss.ID)
			m.mu.Unlock()
			r.close()
			<-r.done
			if _, err := m.get(ss.ID); err != nil {
				t.Fatal("resume replaced native permission policy", err)
			}
		})
	}
}
