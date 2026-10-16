package agent

import (
	"agentdeck/internal/store"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func statusFixture(t *testing.T) (*Follower, string, string) {
	t.Helper()
	n := setup(t)
	root, index := t.TempDir(), t.TempDir()
	project := filepath.Join(root, "project")
	os.MkdirAll(project, 0700)
	meta, _ := json.Marshal(claudeDesktopRecord{SessionID: "local_state", CLI: "native", CWD: project, Title: "Synthetic", Activity: time.Now().Add(time.Hour).UnixMilli()})
	os.WriteFile(filepath.Join(index, "local_state.json"), meta, 0600)
	path := filepath.Join(project, "native.jsonl")
	os.WriteFile(path, nil, 0600)
	return &Follower{Normalizer: n, Roots: []Root{{"claude", root, "log"}}, ClaudeIndex: index, HistoryBytes: 8 << 20}, path, project
}
func appendStatusFixture(t *testing.T, path string, d map[string]any) {
	t.Helper()
	d["sessionId"] = "native"
	b, _ := json.Marshal(d)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	f.Write(append(b, '\n'))
}
func expectClaudeStatus(t *testing.T, f *Follower, status string) {
	t.Helper()
	var ss store.Session
	if err := f.Normalizer.Store.Get("sessions", "native", &ss); err != nil || ss.Status != status {
		t.Fatalf("status expected %s got %s err=%v", status, ss.Status, err)
	}
}
func TestClaudeSidebarStatusWithoutHydratingAndIndependentClock(t *testing.T) {
	f, path, _ := statusFixture(t)
	at := time.Now().Add(-time.Minute)
	row := func(kind, content, stop string, step int) map[string]any {
		return map[string]any{"type": kind, "timestamp": at.Add(time.Duration(step) * time.Second).UTC().Format(time.RFC3339Nano), "uuid": UUID(), "message": map[string]any{"role": kind, "content": content, "stop_reason": stop}}
	}
	appendStatusFixture(t, path, row("user", "A new turn", "", 0))
	f.Scan()
	expectClaudeStatus(t, f, "running")
	appendStatusFixture(t, path, row("assistant", "Done", "end_turn", 1))
	f.Scan()
	expectClaudeStatus(t, f, "completed")
	appendStatusFixture(t, path, row("user", "Another turn", "", 2))
	f.Scan()
	expectClaudeStatus(t, f, "running")
	tool := row("user", "", "", 3)
	obj(tool["message"])["content"] = []any{map[string]any{"type": "tool_result", "content": "Output"}}
	appendStatusFixture(t, path, tool)
	f.Scan()
	expectClaudeStatus(t, f, "running")
	appendStatusFixture(t, path, row("user", "[Request interrupted by user]", "", 4))
	f.Scan()
	expectClaudeStatus(t, f, "interrupted")
	if f.Normalizer.Store.LastSeq() != 0 {
		t.Fatal("sidebar status loaded transcript")
	}
	if err := f.LoadClaudeHistory("native"); err != nil {
		t.Fatal(err)
	}
	expectClaudeStatus(t, f, "interrupted")
}

func TestClaudeQuotaErrorIsFailureInSidebarAndHydratedHistory(t *testing.T) {
	f, path, _ := statusFixture(t)
	appendStatusFixture(t, path, map[string]any{"type": "assistant", "timestamp": time.Now().UTC().Format(time.RFC3339Nano), "uuid": UUID(), "isApiErrorMessage": true, "error": "rate_limit", "message": map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "text", "text": "Weekly limit reached"}}, "stop_reason": "stop_sequence"}})
	f.Scan()
	expectClaudeStatus(t, f, "failed")
	if e := f.LoadClaudeHistory("native"); e != nil {
		t.Fatal(e)
	}
	expectClaudeStatus(t, f, "failed")
}

func TestClaudeHistoricalUnfinishedTurnHasNoLiveExecutorOnFreshStart(t *testing.T) {
	f, path, _ := statusFixture(t)
	f.ClaudeProcesses = t.TempDir() // A readable, empty native registry.
	f.ClaudeProcessSnapshot = func() (map[int]string, error) { return map[int]string{}, nil }
	appendStatusFixture(t, path, map[string]any{"type": "assistant", "timestamp": time.Now().Add(-30 * 24 * time.Hour).UTC().Format(time.RFC3339Nano), "message": map[string]any{"role": "assistant", "stop_reason": "tool_use", "content": []any{map[string]any{"type": "tool_use", "name": "Read"}}}})
	f.Scan()
	expectClaudeStatus(t, f, "interrupted")
	if f.Normalizer.Store.LastSeq() != 0 {
		t.Fatal("status observation imported history")
	}
	// Opening an old transcript must not revive its historical spinner.
	if err := f.LoadClaudeHistory("native"); err != nil {
		t.Fatal(err)
	}
	f.Scan()
	expectClaudeStatus(t, f, "interrupted")
}

func TestClaudeUnknownLiveRegistryRecordDoesNotInventAnExit(t *testing.T) {
	f, path, _ := statusFixture(t)
	f.ClaudeProcesses = t.TempDir()
	f.ClaudeProcessSnapshot = func() (map[int]string, error) {
		return map[int]string{42: "Fri Oct 9 01:57:18 2026"}, nil
	}
	os.WriteFile(filepath.Join(f.ClaudeProcesses, "42.json"), []byte("{"), 0600)
	appendStatusFixture(t, path, map[string]any{"type": "assistant", "timestamp": time.Now().UTC().Format(time.RFC3339Nano), "message": map[string]any{"role": "assistant", "stop_reason": "tool_use"}})
	f.Scan()
	expectClaudeStatus(t, f, "running")
}
func TestClaudeNativeProcessStatusBirthTimeWaitingAndExit(t *testing.T) {
	f, path, _ := statusFixture(t)
	processDir := t.TempDir()
	f.ClaudeProcesses = processDir
	at := time.Now().Add(-time.Minute)
	appendStatusFixture(t, path, map[string]any{"type": "assistant", "timestamp": at.UTC().Format(time.RFC3339Nano), "message": map[string]any{"role": "assistant", "stop_reason": "tool_use", "content": []any{map[string]any{"type": "tool_use", "name": "Read"}}}})
	processes := map[int]string{42: "Fri Oct 9 01:57:18 2026"}
	f.ClaudeProcessSnapshot = func() (map[int]string, error) { return processes, nil }
	record := claudeProcessRecord{PID: 42, Session: "native", Start: "Fri Oct  9 01:57:18 2026", Status: "idle", At: at.Add(time.Second).UnixMilli()}
	target := filepath.Join(processDir, "42.json")
	write := func() { b, _ := json.Marshal(record); os.WriteFile(target, b, 0600) }
	write()
	f.Scan()
	expectClaudeStatus(t, f, "idle")
	record.Status = "busy"
	record.At++
	write()
	f.Scan()
	expectClaudeStatus(t, f, "running")
	record.Status = "waiting"
	record.At++
	write()
	f.Scan()
	expectClaudeStatus(t, f, "waiting")
	os.WriteFile(target, []byte("{"), 0600)
	f.Scan()
	expectClaudeStatus(t, f, "waiting")
	os.Remove(target)
	f.Scan()
	expectClaudeStatus(t, f, "waiting") // still verified alive
	processes[42] = "Sat Oct 10 01:57:18 2026"
	f.Scan()
	expectClaudeStatus(t, f, "interrupted") // PID reused
	if f.Normalizer.Store.LastSeq() != 0 {
		t.Fatal("native status imported history")
	}
	// A stale registry row cannot replace current native log evidence.
	write()
	appendStatusFixture(t, path, map[string]any{"type": "assistant", "timestamp": time.Now().Add(time.Second).UTC().Format(time.RFC3339Nano), "message": map[string]any{"role": "assistant", "stop_reason": "end_turn", "content": "Done"}})
	f.Scan()
	expectClaudeStatus(t, f, "completed")
}

// Explicit read-only native acceptance; never creates a turn or imports content.
func TestRealClaudeNativeStatusReadOnly(t *testing.T) {
	root := os.Getenv("AGENTDECK_CLAUDE_STATUS_ROOT")
	index := os.Getenv("AGENTDECK_CLAUDE_STATUS_INDEX")
	if root == "" || index == "" {
		t.Skip("explicit native status acceptance only")
	}
	n := setup(t)
	f := &Follower{Normalizer: n, Roots: []Root{{"claude", root, "log"}}, ClaudeIndex: index}
	start := time.Now()
	f.init()
	f.refreshClaudeCatalog()
	f.refreshClaudeStatuses()
	f.refreshClaudeProcessStatuses()
	if f.claude == nil || len(f.claude.processOwners) == 0 {
		t.Fatal("no PID and birth-time verified native owner")
	}
	matched := 0
	for id, record := range f.claude.processOwners {
		var ss store.Session
		if n.Store.Get("sessions", id, &ss) == nil && ss.Status == nativeClaudeStatus(record.Status) {
			matched++
		}
	}
	if matched == 0 || n.Store.LastSeq() != 0 {
		t.Fatal("native status was not observed without transcript ingestion")
	}
	interrupted := 0
	for id := range f.claude.orphanSeen {
		var ss store.Session
		if n.Store.Get("sessions", id, &ss) != nil {
			t.Fatal("orphan status missing")
		}
		if ss.Status == "running" || ss.Status == "waiting" || ss.Status == "pending" {
			t.Fatal("historical unowned execution still appears active")
		}
		if ss.Status == "interrupted" {
			interrupted++
		}
	}
	t.Logf("verified owners=%d matched current status=%d historical interrupted=%d raw events=%d elapsed=%s", len(f.claude.processOwners), matched, interrupted, n.Store.LastSeq(), time.Since(start).Round(time.Millisecond))
}
