package agent

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestOpenCodeModelVariantAndMultimodalInput(t *testing.T) {
	m := connectorManager(t)
	m.Binary = "/owner/agentdeck"
	m.ControlDir = t.TempDir()
	project := m.Projects()[0].Path
	var body map[string]any
	statusReads := 0
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("directory") != project {
			t.Error("project scope changed")
		}
		switch r.URL.Path {
		case "/provider":
			w.Write([]byte(`{"connected":["owned"],"default":{"owned":"vision"},"all":[{"id":"owned","key":"API_SECRET_SENTINEL","options":{"headers":{"Authorization":"SECRET_HEADER"}},"models":{"vision":{"id":"vision","name":"Vision","capabilities":{"input":{"image":true}},"variants":{"thinking-2048":{},"low":{}}}}},{"id":"offline","models":{"hidden":{"id":"hidden"}}}]}`))
		case "/session/status":
			statusReads++
			w.Write([]byte(`{"native":{"type":"busy"}}`))
		case "/session/native/prompt_async":
			json.NewDecoder(r.Body).Decode(&body)
			w.WriteHeader(204)
		default:
			http.NotFound(w, r)
		}
	}))
	defer api.Close()
	o := newOpenCode(m, HTTPAgentConfig{URL: api.URL})
	m.Connectors = map[string]Connector{"opencode": o}
	ss := newExternalSession("native", "opencode", project, "opencode-api")
	ss.Status = "running"
	m.Store.Put("sessions", ss.ID, ss)
	choices, e := m.Models("opencode", ss.ID)
	if e != nil || len(choices) != 1 || !choices[0].Images {
		t.Fatal("catalog failed", e)
	}
	b, _ := json.Marshal(choices)
	if strings.Contains(string(b), "SECRET") || strings.Contains(string(b), "offline") {
		t.Fatal("credentials or disconnected models leaked")
	}
	files := []InputAttachment{{ID: UUID(), Name: "pixel.png", MIME: "image/png", Data: []byte{1, 2, 3}}, {ID: UUID(), Name: "report.txt", MIME: "text/plain", Data: []byte("private file bytes")}}
	if e = m.SendInput(ss.ID, "input-check", UUID(), TurnOptions{Model: "owned/vision", Effort: "thinking-2048"}, files); e != nil {
		t.Fatal(e)
	}
	if str(obj(body["model"])["providerID"]) != "owned" || str(obj(body["model"])["modelID"]) != "vision" || body["variant"] != "thinking-2048" {
		t.Fatal("native generation parameters dropped")
	}
	if statusReads != 0 {
		t.Fatal("native asynchronous submission was blocked by a busy-status probe")
	}
	parts := arr(body["parts"])
	if len(parts) != 2 || obj(parts[1])["url"] != "data:image/png;base64,"+base64.StdEncoding.EncodeToString(files[0].Data) {
		t.Fatal("native image bytes dropped")
	}
	text := str(obj(parts[0])["text"])
	if !strings.Contains(text, "input-file") || !strings.Contains(text, files[1].ID) || strings.Contains(text, "private file bytes") {
		t.Fatal("encrypted file reference missing")
	}
	if e = m.SendInput(ss.ID, "reject", UUID(), TurnOptions{Model: "owned/vision", Effort: "invented"}, nil); e == nil {
		t.Fatal("unavailable variant accepted")
	}
	if _, e = m.InputCapabilities("claude", ss.ID, true); e == nil {
		t.Fatal("cross-agent catalog accepted")
	}
}
func TestACPNegotiatedModelsImagesAndDependentReasoning(t *testing.T) {
	for _, name := range []string{"qoder", "trae"} {
		t.Run(name, func(t *testing.T) {
			m := connectorManager(t)
			m.Binary = "/owner/agentdeck"
			m.ControlDir = t.TempDir()
			fixture, _ := filepath.Abs("../../../scripts/fixture_acp.py")
			c := newACP(m, name, ACPConfig{Command: "python3", Args: []string{fixture, "--inputs"}})
			m.Connectors = map[string]Connector{name: c}
			ss, e := m.Create(name, m.Projects()[0].ID, "")
			if e != nil {
				t.Fatal(e)
			}
			choices, e := m.Models(name, ss.ID)
			if e != nil || len(choices) != 2 || len(choices[0].Efforts) != 2 {
				t.Fatal("grouped model config lost", e)
			}
			caps, e := m.InputCapabilities(name, ss.ID, true)
			if e != nil || caps["images"] != true || caps["model_switch"] != true {
				t.Fatal("negotiated capability lost", e)
			}
			files := []InputAttachment{{ID: UUID(), Name: "pixel.png", MIME: "image/png", Data: []byte{1, 2, 3}}, {ID: UUID(), Name: "report.txt", MIME: "text/plain", Data: []byte("file bytes")}}
			if e = m.SendInput(ss.ID, "input-check", UUID(), TurnOptions{Model: "model-a", Effort: "high"}, files); e != nil {
				t.Fatal(e)
			}
			awaitConnector(t, m, ss.ID, "message", "native multimodal inputs received", "")
			awaitConnector(t, m, ss.ID, "status", "", "completed")
			r, _ := c.get(m.Context, ss)
			for i := 0; i < 100; i++ {
				r.mu.Lock()
				busy := r.busy
				r.mu.Unlock()
				if !busy {
					break
				}
				time.Sleep(time.Millisecond * 10)
			}
			if e = m.SendInput(ss.ID, "must not prompt", UUID(), TurnOptions{Model: "model-b", Effort: "high"}, nil); e == nil {
				t.Fatal("invalid dependent reasoning accepted")
			}
			choices, e = m.Models(name, ss.ID)
			if e != nil || len(choices[1].Efforts) != 1 || choices[1].Efforts[0] != "low" {
				t.Fatal("dependent configuration not refreshed", e)
			}
			c.Close()
			r, e = c.get(m.Context, ss)
			if e != nil {
				t.Fatal(e)
			}
			r.mu.Lock()
			restored := len(r.configOptions)
			r.mu.Unlock()
			if restored != 3 {
				t.Fatal("load omitted model metadata")
			}
		})
	}
}
func TestACPRejectsImagesWithoutNegotiatedCapability(t *testing.T) {
	m := connectorManager(t)
	m.Binary = "/owner/agentdeck"
	m.ControlDir = t.TempDir()
	fixture, _ := filepath.Abs("../../../scripts/fixture_acp.py")
	c := newACP(m, "qoder", ACPConfig{Command: "python3", Args: []string{fixture, "--inputs", "--images-off"}})
	m.Connectors = map[string]Connector{"qoder": c}
	ss, e := m.Create("qoder", m.Projects()[0].ID, "")
	if e != nil {
		t.Fatal(e)
	}
	if e = m.SendInput(ss.ID, "image", UUID(), TurnOptions{}, []InputAttachment{{ID: UUID(), Name: "pixel.png", MIME: "image/png", Data: []byte{1, 2, 3}}}); e == nil {
		t.Fatal("unsupported image sent")
	}
}
func TestWorkBuddyVoiceAndReadOnlyFileReference(t *testing.T) {
	m := connectorManager(t)
	m.Binary = "/owner/agentdeck"
	m.ControlDir = t.TempDir()
	m.VoiceCommand = []string{"configured"}
	token := workBuddyToken{Access: "fixture", OpenID: "fixture", Expires: time.Now().Add(time.Hour).Unix()}
	m.Store.Put("agentconfig", "workbuddy/oauth", token)
	var content string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/localassistant":
			w.Write([]byte(`{"data":{"online":true}}`))
		case "/localassistant/message":
			var v map[string]any
			json.NewDecoder(r.Body).Decode(&v)
			if v["msg_type"] != "text" {
				t.Error("invented native file operation")
			}
			content = str(v["content"])
			w.Write([]byte(`{"data":{"message_id":"fixture"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer api.Close()
	c := newWorkBuddy(m, WorkBuddyConfig{API: api.URL})
	m.Connectors = map[string]Connector{"workbuddy": c}
	ss := workBuddySession(token)
	m.Store.Put("sessions", ss.ID, ss)
	caps, e := m.InputCapabilities("workbuddy", ss.ID, true)
	if e != nil || caps["voice"] != true || caps["files"] != true || caps["images"] != false || caps["model_switch"] != false {
		t.Fatal("WorkBuddy capabilities invented", e)
	}
	if e = m.SendInput(ss.ID, "read", UUID(), TurnOptions{}, []InputAttachment{{ID: UUID(), Name: "report.txt", MIME: "text/plain", Data: []byte("private bytes")}}); e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(content, "input-file") || strings.Contains(content, "private bytes") {
		t.Fatal("read-only reference not sent")
	}
	if choices, e := m.Models("workbuddy", ss.ID); e != nil || len(choices) != 0 {
		t.Fatal("fake WorkBuddy model catalog")
	}
}
