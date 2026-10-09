package agent

import (
	"agentdeck/internal/store"
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGenerationAndMultimodalProtocol(t *testing.T) {
	fixture, e := os.ReadFile("../../../scripts/fixture_runtime.py")
	if e != nil {
		t.Fatal(e)
	}
	t.Setenv("AGENTDECK_FIXTURE_SESSION_DIR", t.TempDir())
	bin := t.TempDir()
	for _, a := range []string{"codex", "claude"} {
		if e = os.WriteFile(filepath.Join(bin, a), fixture, 0700); e != nil {
			t.Fatal(e)
		}
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	root := t.TempDir()
	state := t.TempDir()
	db, e := store.New(state)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	m := &Manager{Store: db, Normalizer: &Normalizer{Store: db}, Roots: []string{root}, Binary: "/fixture/agentdeck", ControlDir: state}
	defer m.Close()
	for _, a := range []string{"codex", "claude"} {
		t.Run(a, func(t *testing.T) {
			ss, e := m.Create(a, m.Projects()[0].ID, "")
			if e != nil {
				t.Fatal(e)
			}
			choices, e := m.Models(a, ss.ID)
			if e != nil || len(choices) != 1 || choices[0].ID != "fixture-model" {
				t.Fatalf("catalog: %v", e)
			}
			if e = m.SendInput(ss.ID, "generation-check", UUID(), TurnOptions{Model: "fixture-model", Effort: "max"}, nil); e == nil {
				t.Fatal("unsupported effort accepted")
			}
			for _, eff := range []string{"low", "high"} {
				before := m.runs[ss.ID]
				files := []InputAttachment{{ID: UUID(), Name: "report.txt", MIME: "text/plain", Data: []byte("private file")}, {ID: UUID(), Name: "pixel.png", MIME: "image/png", Data: []byte{1, 2, 3}}}
				if e = m.SendInput(ss.ID, "generation-check", UUID(), TurnOptions{Model: "fixture-model", Effort: eff}, files); e != nil {
					t.Fatal(e)
				}
				deadline := time.Now().Add(5 * time.Second)
				found := false
				for time.Now().Before(deadline) {
					evs, _ := db.Events(ss.ID, 0, 200)
					for _, ev := range evs {
						if ev.Role == "assistant" && strings.Contains(ev.Text, "model=fixture-model effort="+eff) {
							found = true
						}
					}
					r := m.runs[ss.ID]
					r.mu.Lock()
					busy := r.busy
					r.mu.Unlock()
					if found && !busy {
						break
					}
					time.Sleep(10 * time.Millisecond)
				}
				if !found {
					t.Fatal("settings did not reach native protocol")
				}
				if a == "claude" && before == m.runs[ss.ID] {
					t.Fatal("Claude effort did not resume process")
				}
				r := m.runs[ss.ID]
				if r.session.ID != ss.ID {
					t.Fatal("session identity changed")
				}
				if m.Options(ss.ID).Effort != eff {
					t.Fatal("preferences not persisted")
				}
			}
			entries, _ := os.ReadDir(root)
			if len(entries) != 0 {
				t.Fatal("plaintext working files materialized")
			}
		})
	}
}
func TestRecordingValidationAndTranscription(t *testing.T) {
	wav := make([]byte, 44+32000)
	copy(wav, "RIFF")
	binary.LittleEndian.PutUint32(wav[4:], uint32(len(wav)-8))
	copy(wav[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(wav[16:], 16)
	binary.LittleEndian.PutUint16(wav[20:], 1)
	binary.LittleEndian.PutUint16(wav[22:], 1)
	binary.LittleEndian.PutUint32(wav[24:], 16000)
	binary.LittleEndian.PutUint32(wav[28:], 32000)
	binary.LittleEndian.PutUint16(wav[32:], 2)
	binary.LittleEndian.PutUint16(wav[34:], 16)
	copy(wav[36:], "data")
	binary.LittleEndian.PutUint32(wav[40:], 32000)
	if e := ValidateRecording(wav); e != nil {
		t.Fatal(e)
	}
	bad := append([]byte(nil), wav...)
	bad[4] = 0
	if ValidateRecording(bad) == nil {
		t.Fatal("wrong RIFF length accepted")
	}
	m := &Manager{Context: context.Background(), VoiceCommand: []string{"python3", "-c", "import sys,json; a=sys.stdin.buffer.read(); print(json.dumps({'text':'fixture voice'}))"}}
	text, e := m.Transcribe(wav)
	if e != nil || text != "fixture voice" {
		t.Fatal("stdin transcription failed", e)
	}
	m.voiceMu.Lock()
	_, e = m.Transcribe(wav)
	m.voiceMu.Unlock()
	if e == nil {
		t.Fatal("queued unbounded transcription")
	}
}
func TestAttachmentInputNoPlaintext(t *testing.T) {
	m := &Manager{Binary: "/some dir/agent'deck", ControlDir: "/private/state"}
	inputs, display, e := m.prepareInput("session", "read", "r", []InputAttachment{{ID: "id", Name: "[report].txt", MIME: "text/plain", Data: []byte("secret")}})
	if e != nil || len(inputs) != 1 || !strings.Contains(display, "agentdeck-file:id") || !strings.Contains(display, "--session 'session'") || strings.Contains(display, "secret") {
		t.Fatal("stable encrypted reference missing", e)
	}
	if shellQuote("a'b") != "'a'\\''b'" {
		t.Fatal("shell quoting")
	}
}
