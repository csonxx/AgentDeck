package server

import (
	"agentdeck/internal/feedback"
	"agentdeck/internal/store"
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func feedbackFixture() feedback.Submission {
	return feedback.Submission{ID: "AD-FB-" + strings.Repeat("a", 32), Message: "Fixture send stalled", AppVersion: "0.4.51+9068", Platform: "android", Diagnostics: []feedback.Diagnostic{{Time: time.Now().UTC(), Component: "app", Code: "send_total", RequestID: strings.Repeat("b", 32), DurationMS: 21000}}}
}

func TestFeedbackViaEncryptedPhoneIndependentOfAgentAndDatabase(t *testing.T) {
	s, ts := setupServer(t)
	p := pairPhone(t, s, ts)
	p.connect(t, ts, s)
	p.read(t, "sessions")
	v := feedbackFixture()
	raw, _ := json.Marshal(v)
	for i := 0; i < 2; i++ {
		p.send(t, Request{Method: "feedback_submit", ID: strings.Repeat("c", 32), Feedback: raw})
		r := p.read(t, "result")["result"].(map[string]any)
		if r["id"] != v.ID || r["stored"] != "workstation" {
			t.Fatal("feedback acknowledgement invalid")
		}
	}
	rows, e := s.Store.Values("feedback")
	if e != nil || len(rows) != 1 || s.Store.LastSeq() != 0 {
		t.Fatal("feedback duplicated or modified chat")
	}
	other := pairPhone(t, s, ts)
	other.connect(t, ts, s)
	other.read(t, "sessions")
	other.send(t, Request{Method: "feedback_submit", ID: strings.Repeat("d", 32), Feedback: raw})
	other.read(t, "error")
	public, e := ts.Client().Get(ts.URL + "/feedback/" + v.ID)
	if e != nil {
		t.Fatal(e)
	}
	public.Body.Close()
	if public.StatusCode != 404 {
		t.Fatal("owner feedback exposed publicly")
	}
	m := http.NewServeMux()
	s.feedbackAdminRoutes(m)
	w := httptest.NewRecorder()
	m.ServeHTTP(w, httptest.NewRequest("GET", "/feedback/"+v.ID, nil))
	var report feedback.Report
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &report) != nil || report.Diagnostics[0].RequestID != strings.Repeat("b", 32) {
		t.Fatal("owner cannot correlate report")
	}
	changed := v
	changed.Message = "different"
	raw, _ = json.Marshal(changed)
	if _, e = s.submitFeedback(p.device, raw); e == nil {
		t.Fatal("same ID accepted different payload")
	}
}

func TestLocalFeedbackEncryptedBoundedAndExpiring(t *testing.T) {
	dir := t.TempDir()
	db, e := store.New(dir)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	s := New(db, "", "", time.Minute)
	if e = db.Put("devices", "paired", store.Device{ID: "paired"}); e != nil {
		t.Fatal(e)
	}
	v := feedbackFixture()
	raw, _ := json.Marshal(v)
	if _, e = s.submitFeedback("paired", raw); e != nil {
		t.Fatal(e)
	}
	stored, e := os.ReadFile(filepath.Join(dir, "vault.db"))
	if e != nil {
		t.Fatal(e)
	}
	if bytes.Contains(stored, []byte(v.Message)) || bytes.Contains(stored, []byte(v.Diagnostics[0].RequestID)) {
		t.Fatal("plaintext diagnostics on disk")
	}
	var row localFeedback
	db.Get("feedback", v.ID, &row)
	row.Report.Expires = time.Now().Add(-time.Second)
	db.Put("feedback", v.ID, row)
	rows, e := s.feedbackRows()
	if e != nil || len(rows) != 0 {
		t.Fatal("expired report retained")
	}
	bad := []byte(`{"id":"AD-FB-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","message":"fixture","app_version":"0.4.51","platform":"android","raw_logs":"credential"}`)
	if _, e = s.submitFeedback("paired", bad); e == nil {
		t.Fatal("raw logs accepted")
	}
	if _, e = s.submitFeedback("revoked", raw); e == nil {
		t.Fatal("unpaired device submitted feedback")
	}
}
