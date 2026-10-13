package agent

import (
	"agentdeck/internal/claudedesktop"
	"agentdeck/internal/store"
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Requires an explicitly authorized, isolated native Desktop chat. Never run
// against an arbitrary recent conversation or from the ordinary verify suite.
func TestRealClaudeDesktopImageApprovalAndBusyInput(t *testing.T) {
	id := os.Getenv("AGENTDECK_REAL_CLAUDE_DESKTOP_NATIVE_SESSION")
	if id == "" {
		t.Skip("explicit isolated Desktop acceptance only")
	}
	home, _ := os.UserHomeDir()
	n := setup(t)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	m := &Manager{Store: n.Store, Normalizer: n, Context: ctx, Roots: []string{filepath.Join(home, "goworkspace", "src")}, ClaudeDesktopDebugger: true, ClaudeDesktopSessions: filepath.Join(home, "Library", "Application Support", "Claude", "claude-code-sessions"), ClaudeGoalRoot: filepath.Join(home, ".claude", "projects")}
	defer m.Close()
	ss := store.Session{ID: id, Agent: "claude", Project: filepath.Join(home, "goworkspace", "src", "csonxx"), Source: "claude-desktop", Status: "idle"}
	if err := m.Store.Put("sessions", id, ss); err != nil {
		t.Fatal(err)
	}
	owner, err := m.claudeHandoffOwner(id, 0)
	if err != nil || owner == nil || owner.EntryPoint != "claude-desktop" {
		t.Fatal("original Desktop owner unavailable")
	}
	follower := &Follower{Normalizer: n, Roots: []Root{{Agent: "claude", Path: m.ClaudeGoalRoot, Source: "log"}}, ClaudeIndex: m.ClaudeDesktopSessions, HistoryBytes: 8 << 20}
	m.LoadHistory = follower.LoadClaudeHistory
	target, found, err := m.claudeDesktopTarget(ss)
	if err != nil || !found {
		t.Fatal("isolated catalog unavailable")
	}
	readState := func() claudedesktop.State {
		t.Helper()
		b, e := claudedesktop.Open(ctx)
		if e != nil {
			t.Fatal("debugger unavailable")
		}
		defer b.Close()
		s, e := b.State(ctx, target)
		if e != nil {
			t.Fatal("native state unavailable")
		}
		return s
	}
	send := func(text string, files []InputAttachment) {
		t.Helper()
		if e := m.SendDelivery(id, text, UUID(), TurnOptions{}, files, "send"); e != nil {
			t.Fatal("native send failed", e)
		}
	}
	reply := func(marker string) {
		t.Helper()
		deadline := time.Now().Add(70 * time.Second)
		for time.Now().Before(deadline) && ctx.Err() == nil {
			_ = m.LoadHistory(id)
			events, e := m.Store.Events(id, 0, 500)
			if e != nil {
				t.Fatal(e)
			}
			for _, v := range events {
				if v.Role == "assistant" && strings.EqualFold(strings.TrimSpace(v.Text), marker) {
					return
				}
			}
			time.Sleep(300 * time.Millisecond)
		}
		t.Fatal("expected original-session reply not observed", marker)
	}
	img := image.NewRGBA(image.Rect(0, 0, 256, 256))
	for y := 0; y < 256; y++ {
		for x := 0; x < 256; x++ {
			img.Set(x, y, color.RGBA{0, 0, 255, 255})
		}
	}
	var binary bytes.Buffer
	_ = png.Encode(&binary, img)
	send("Look at the attached image. Do not use tools. Reply only AD_IMAGE_ followed by the main color name in English, in uppercase.", []InputAttachment{{ID: UUID(), Name: "acceptance.png", MIME: "image/png", Data: binary.Bytes()}})
	reply("AD_IMAGE_BLUE")
	t.Log("PASS original human image input and model image answer")
	send("Use AskUserQuestion now. Ask exactly AD_APPROVAL_PICK with two options AD_ONE and AD_TWO. Wait for my choice. After the answer reply only AD_APPROVAL_DONE.", nil)
	var pending claudedesktop.Permission
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		s := readState()
		for _, p := range s.Permissions {
			if p.Session == target.Host && p.Tool == "AskUserQuestion" {
				pending = p
			}
		}
		if pending.ID != "" {
			break
		}
		time.Sleep(300 * time.Millisecond)
	}
	if pending.ID == "" {
		t.Fatal("native AskUserQuestion not observed")
	}
	if handled, e := m.controlClaudeDesktop(ss, pending.ID, "accept", `{"AD_APPROVAL_PICK":"AD_ONE"}`); !handled || e != nil {
		t.Fatal("native permission reply failed", e)
	}
	reply("AD_APPROVAL_DONE")
	t.Log("PASS native question/one-time response and original-session continuation")
	send("Use Bash to run exactly sleep 12, no other command. Then reply only AD_BUSY_FIRST.", nil)
	running := false
	deadline = time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		s := readState()
		if s.Running {
			running = true
		}
		for _, p := range s.Permissions {
			if p.Tool == "Bash" {
				if _, e := m.controlClaudeDesktop(ss, p.ID, "accept", ""); e != nil {
					t.Fatal(e)
				}
			}
		}
		if running {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if !running {
		t.Fatal("original turn never running")
	}
	send("After the current operation, do not use tools; reply only AD_BUSY_SECOND.", nil)
	reply("AD_BUSY_SECOND")
	t.Log("PASS additional human input accepted during running turn")
	latest, e := m.claudeHandoffOwner(id, 0)
	if e != nil || latest == nil || latest.PID != owner.PID || latest.Start != owner.Start || len(m.runs) != 0 {
		t.Fatal("original owner changed or replacement executor created")
	}
	t.Log("PASS original native UUID, PID and birth preserved across image, approval and busy send")
}
