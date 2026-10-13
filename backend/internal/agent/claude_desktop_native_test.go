package agent

import (
	"agentdeck/internal/claudedesktop"
	"agentdeck/internal/store"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type nativeDesktopFixture struct {
	sends       int
	accepted    bool
	drop        bool
	failed      bool
	seen        claudedesktop.Target
	uuid        string
	text        string
	snapshots   int
	acceptAfter int
}

func (f *nativeDesktopFixture) Close() {}
func (f *nativeDesktopFixture) Snapshot(_ context.Context, t claudedesktop.Target, uuid string) (claudedesktop.Snapshot, error) {
	if f.failed {
		return claudedesktop.Snapshot{}, errors.New("closed")
	}
	f.seen = t
	f.uuid = uuid
	f.snapshots++
	if f.acceptAfter > 0 && f.snapshots >= f.acceptAfter {
		f.accepted = true
	}
	return claudedesktop.Snapshot{Found: true, Accepted: f.accepted, Model: "fixture-model", Effort: "extra"}, nil
}
func (f *nativeDesktopFixture) Send(_ context.Context, t claudedesktop.Target, text, uuid string) error {
	f.sends++
	f.seen = t
	f.text = text
	f.uuid = uuid
	if !f.drop {
		f.accepted = true
	}
	return errors.New("lost IPC acknowledgement") // transcript is authoritative
}

func nativeDesktopManager(t *testing.T) (*Manager, store.Session, *nativeDesktopFixture, string) {
	t.Helper()
	n := setup(t)
	project := t.TempDir()
	catalog := t.TempDir()
	ss := store.Session{ID: UUID(), Agent: "claude", Project: project, Source: "claude-desktop", Status: "running"}
	if e := n.Store.Put("sessions", ss.ID, ss); e != nil {
		t.Fatal(e)
	}
	meta := claudeDesktopRecord{SessionID: "local_original", CLI: ss.ID, CWD: project, Origin: project}
	b, _ := json.Marshal(meta)
	path := filepath.Join(catalog, "local_original.json")
	if e := os.WriteFile(path, b, 0600); e != nil {
		t.Fatal(e)
	}
	f := &nativeDesktopFixture{}
	m := &Manager{Store: n.Store, Normalizer: n, Roots: []string{project}, Context: context.Background(), ClaudeDesktopDebugger: true, ClaudeDesktopSessions: catalog,
		ClaudeOwner: func(string, int) error { return nil }, ClaudeProcesses: t.TempDir(), ClaudeProcessSnapshot: func() (map[int]string, error) { return map[int]string{}, nil },
		openClaudeDesktop: func(context.Context) (claudeDesktopClient, error) { return f, nil }}
	t.Cleanup(m.Close)
	return m, ss, f, path
}

type nativeImageFixture struct {
	*nativeDesktopFixture
	images []claudedesktop.Image
}

func (f *nativeImageFixture) SendImages(ctx context.Context, target claudedesktop.Target, text, uuid string, images []claudedesktop.Image) error {
	f.images = images
	return f.Send(ctx, target, text, uuid)
}

func TestClaudeDesktopImageUsesOriginalHumanRequestExactlyOnce(t *testing.T) {
	m, ss, f, _ := nativeDesktopManager(t)
	image := &nativeImageFixture{nativeDesktopFixture: f}
	m.openClaudeDesktop = func(context.Context) (claudeDesktopClient, error) { return image, nil }
	files := []InputAttachment{{ID: "uploaded-image", Name: "image.png", MIME: "image/png", Data: []byte{1, 2, 3}}}
	for i := 0; i < 2; i++ {
		if err := m.SendDelivery(ss.ID, "inspect image", "image-request", TurnOptions{}, files, "send"); err != nil {
			t.Fatal(err)
		}
	}
	if f.sends != 1 || len(image.images) != 1 || image.images[0].Base64 != "AQID" || image.images[0].MIME != "image/png" || f.seen.Native != ss.ID {
		t.Fatal("image missing, duplicate or wrong original session")
	}
	if !strings.Contains(f.text, "agentdeck-file:uploaded-image") {
		t.Fatal("encrypted attachment link missing")
	}
}

func TestClaudeDesktopNativeHumanReceiptNoReplacementOrLocalQueue(t *testing.T) {
	for _, mode := range []string{"send", "queue", "steer"} {
		t.Run(mode, func(t *testing.T) {
			m, ss, f, path := nativeDesktopManager(t)
			request := "native-fixture-request"
			for i := 0; i < 2; i++ {
				if e := m.SendDelivery(ss.ID, "owner input", request, TurnOptions{}, nil, mode); e != nil {
					t.Fatal(e)
				}
			}
			if f.sends != 1 || f.seen.Host != "local_original" || f.seen.Native != ss.ID || f.text != "owner input" || f.uuid != claudeDesktopMessageUUID(ss.ID, request) || len(m.runs) != 0 {
				t.Fatal("wrong target, duplicate or replacement")
			}
			if q, e := m.Queue(ss.ID); e != nil || len(q) != 0 {
				t.Fatal("workstation queue created", e)
			}
			if v, ok := m.NativeSubmission(ss.ID, request).(map[string]string); !ok || v["delivery"] != "submitted" || v["message_id"] != f.uuid {
				t.Fatal("missing receipt")
			}
			// A restarted server acknowledges its persisted receipt without a
			// Desktop process, catalog entry or another send.
			os.Remove(path)
			restarted := &Manager{Store: m.Store, Roots: m.Roots, ClaudeDesktopDebugger: true}
			if e := restarted.SendDelivery(ss.ID, "owner input", request, TurnOptions{}, nil, "send"); e != nil {
				t.Fatal(e)
			}
			if e := restarted.SendDelivery(ss.ID, "changed input", request, TurnOptions{}, nil, "send"); !InputDefinitelyNotSent(e) {
				t.Fatal("request collision accepted")
			}
		})
	}
}

func TestClaudeDesktopNativeUnknownNeverRetriesEvenWhenCatalogDisappears(t *testing.T) {
	m, ss, f, path := nativeDesktopManager(t)
	ctx, cancel := context.WithTimeout(context.Background(), 350*time.Millisecond)
	defer cancel()
	m.Context = ctx
	f.drop = true
	request := "ambiguous"
	if e := m.SendDelivery(ss.ID, "owner input", request, TurnOptions{}, nil, "send"); e == nil || InputDefinitelyNotSent(e) {
		t.Fatal("false acceptance", e)
	}
	if e := m.SendDelivery(ss.ID, "owner input", request, TurnOptions{}, nil, "send"); e == nil || InputDefinitelyNotSent(e) {
		t.Fatal("ambiguous dispatch retryable", e)
	}
	os.Remove(path)
	if e := m.SendDelivery(ss.ID, "owner input", request, TurnOptions{}, nil, "send"); e == nil || InputDefinitelyNotSent(e) {
		t.Fatal("missing owner became retryable", e)
	}
	if f.sends != 1 || len(m.runs) != 0 {
		t.Fatal("resend or replacement")
	}
}

func TestClaudeDesktopNativeLateReceiptRecoversWithoutResend(t *testing.T) {
	m, ss, f, _ := nativeDesktopManager(t)
	ctx, cancel := context.WithTimeout(context.Background(), 350*time.Millisecond)
	defer cancel()
	m.Context = ctx
	f.drop = true
	_ = m.SendDelivery(ss.ID, "owner input", "late", TurnOptions{}, nil, "send")
	m.Context = context.Background()
	f.accepted = true
	if e := m.SendDelivery(ss.ID, "owner input", "late", TurnOptions{}, nil, "send"); e != nil {
		t.Fatal(e)
	}
	if f.sends != 1 {
		t.Fatal("resend")
	}
}

func TestClaudeDesktopNativeWaitsForAsynchronousHumanRecord(t *testing.T) {
	m, ss, f, _ := nativeDesktopManager(t)
	f.drop = true
	f.acceptAfter = 28
	if e := m.SendDelivery(ss.ID, "owner input", "delayed-native-flush", TurnOptions{}, nil, "send"); e != nil {
		t.Fatal("human record rejected before request deadline", e)
	}
	if f.sends != 1 || f.snapshots < 28 {
		t.Fatal("did not wait for exact record or resent input")
	}
}

func TestClaudeDesktopNativeRejectsWrongProjectAndDisconnectedDebugger(t *testing.T) {
	for _, kind := range []string{"project", "duplicate", "archived", "writable", "disconnected", "parameters"} {
		t.Run(kind, func(t *testing.T) {
			m, ss, f, path := nativeDesktopManager(t)
			var meta claudeDesktopRecord
			b, _ := os.ReadFile(path)
			json.Unmarshal(b, &meta)
			o := TurnOptions{}
			switch kind {
			case "project":
				meta.Origin = t.TempDir()
			case "archived":
				meta.Archived = true
			case "disconnected":
				m.openClaudeDesktop = func(context.Context) (claudeDesktopClient, error) { return nil, errors.New("closed") }
			case "parameters":
				o.Model = "different-model"
			case "duplicate":
				meta.SessionID = "local_duplicate"
				b, _ = json.Marshal(meta)
				os.WriteFile(filepath.Join(filepath.Dir(path), "local_duplicate.json"), b, 0600)
				meta.SessionID = "local_original"
			}
			b, _ = json.Marshal(meta)
			os.WriteFile(path, b, 0600)
			if kind == "writable" {
				os.Chmod(path, 0666)
			}
			if e := m.SendDelivery(ss.ID, "owner input", "rejected", o, nil, "send"); !InputDefinitelyNotSent(e) {
				t.Fatal("unsafe target accepted", e)
			}
			if f.sends != 0 || len(m.runs) != 0 {
				t.Fatal("dispatched or replacement")
			}
		})
	}
}

// Explicit opt-in only; never runs against the user's account in verify.sh.
// This proves native human-input transport. A rate-limited model is explicitly
// not counted as successful inference or phone UI acceptance.
func TestRealClaudeDesktopOwnerInput(t *testing.T) {
	id := os.Getenv("AGENTDECK_REAL_CLAUDE_DESKTOP_NATIVE_SESSION")
	if id == "" {
		t.Skip("requires an explicitly authorized isolated Desktop test session")
	}
	home, e := os.UserHomeDir()
	if e != nil {
		t.Fatal(e)
	}
	n := setup(t)
	m := &Manager{Store: n.Store, Normalizer: n, Roots: []string{filepath.Join(home, "goworkspace", "src")}, Context: context.Background(), ClaudeDesktopDebugger: true,
		ClaudeDesktopSessions: filepath.Join(home, "Library", "Application Support", "Claude", "claude-code-sessions"), ClaudeGoalRoot: filepath.Join(home, ".claude", "projects")}
	ss := store.Session{ID: id, Agent: "claude", Project: filepath.Join(home, "goworkspace", "src", "csonxx"), Source: "claude-desktop", Status: "idle"}
	if e = m.Store.Put("sessions", id, ss); e != nil {
		t.Fatal(e)
	}
	owner, e := m.claudeHandoffOwner(id, 0)
	if e != nil || owner == nil || owner.EntryPoint != "claude-desktop" {
		t.Fatal("isolated Desktop owner unavailable")
	}
	f := &Follower{Normalizer: n, Roots: []Root{{Agent: "claude", Path: m.ClaudeGoalRoot, Source: "log"}}, ClaudeIndex: m.ClaudeDesktopSessions, HistoryBytes: 8 << 20}
	m.LoadHistory = f.LoadClaudeHistory
	if e = m.LoadHistory(id); e != nil {
		t.Fatal("cannot establish native history baseline")
	}
	baseline := m.Store.LastSeq()
	request := UUID()
	marker := "AD_BACKEND_USER_" + strings.ReplaceAll(request, "-", "")
	input := "Do not use tools, read files, or change anything. Reply only " + marker + "."
	for i := 0; i < 2; i++ {
		if e = m.SendDelivery(id, input, request, TurnOptions{}, nil, "send"); e != nil {
			t.Fatal("native user input not confirmed", e)
		}
	}
	latest, e := m.claudeHandoffOwner(id, 0)
	if e != nil || latest == nil || latest.PID != owner.PID || latest.Start != owner.Start {
		t.Fatal("original process changed")
	}
	landed := false
	quota := false
	reply := false
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		if e = m.LoadHistory(id); e != nil {
			t.Fatal("native history synchronization failed")
		}
		events, e := m.Store.Events(id, baseline, 100)
		if e != nil {
			t.Fatal(e)
		}
		matches := 0
		for _, event := range events {
			if event.Role == "user" && event.Text == input {
				matches++
				landed = true
			}
			if event.Role == "assistant" && strings.Contains(strings.ToLower(event.Text), "limit") {
				quota = true
			}
			if event.Role == "assistant" && strings.TrimSpace(event.Text) == marker {
				reply = true
			}
		}
		if matches > 1 {
			t.Fatal("duplicate native user input")
		}
		if landed && (quota || reply) {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !landed {
		t.Fatal("accepted message not synchronized into encrypted Store")
	}
	if !quota && !reply {
		t.Fatal("neither reply nor explicit quota error synchronized")
	}
	if len(m.runs) != 0 {
		t.Fatal("replacement executor started")
	}
	latest, e = m.claudeHandoffOwner(id, 0)
	if e != nil || latest == nil || latest.PID != owner.PID || latest.Start != owner.Start {
		t.Fatal("original process changed before reply synchronization")
	}
	if quota {
		if os.Getenv("AGENTDECK_REAL_CLAUDE_REQUIRE_INFERENCE") == "1" {
			t.Fatal("transport accepted, but model quota is still exhausted")
		}
		t.Log("PASS transport: human input, original PID/native session, idempotent receipt and quota-error synchronization. INFERENCE PENDING: quota exhausted; no phone UI acceptance claimed.")
	} else {
		t.Log("PASS backend transport and model reply in original Desktop process; phone UI acceptance still separate.")
	}
}
