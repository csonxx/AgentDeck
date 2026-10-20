package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"agentdeck/internal/feedback"
	"agentdeck/internal/relay"
	"agentdeck/internal/secure"
	"agentdeck/internal/store"
)

type feedbackFixture struct {
	scope      feedback.Scope
	submission feedback.Submission
	calls      int
	fail       bool
}

func (f *feedbackFixture) Create(_ context.Context, s feedback.Scope, v feedback.Submission) (feedback.Report, bool, error) {
	if f.fail {
		return feedback.Report{}, false, feedback.ErrUnavailable
	}
	f.scope = s
	f.submission = v
	f.calls++
	return feedback.Report{Submission: v, Status: "new"}, true, nil
}
func (f *feedbackFixture) Get(_ context.Context, s feedback.Scope, id string) (feedback.Report, error) {
	if s != f.scope || id != f.submission.ID {
		return feedback.Report{}, feedback.ErrNotFound
	}
	return feedback.Report{Submission: f.submission, Status: "new"}, nil
}
func (f *feedbackFixture) List(_ context.Context, s feedback.Scope, _ string, _ int) (feedback.Page, error) {
	if s != f.scope {
		return feedback.Page{Items: []feedback.Summary{}}, nil
	}
	return feedback.Page{Items: []feedback.Summary{{ID: f.submission.ID}}}, nil
}
func (*feedbackFixture) Delete(context.Context, feedback.Scope, string) error            { return nil }
func (*feedbackFixture) SetStatus(context.Context, feedback.Scope, string, string) error { return nil }
func (*feedbackFixture) Prune(context.Context) error                                     { return nil }

func TestFeedbackAuthenticatedEncryptedRoutes(t *testing.T) {
	db, e := store.New(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	g := New(db, "https://example.test/agentdeck", "", "system", time.Minute)
	mock := &feedbackFixture{}
	g.Feedback = mock
	var logs bytes.Buffer
	g.ConfigureLogging(&logs, "debug")
	key := secure.Random(32)
	a := Account{ID: secure.ID(), Key: secure.B64.EncodeToString(key), Identity: Identity{Tenant: "tenant-a", Owner: "owner-a"}, Expires: g.Clock.Unix() + 3600}
	db.Put("gwaccounts", a.ID, a)
	h := g.Handler()
	call := func(op string, v any, want int) *httptest.ResponseRecorder {
		t.Helper()
		box, e := secure.SealJSON(key, "account-request/"+a.ID, 0, 0, v)
		if e != nil {
			t.Fatal(e)
		}
		body, _ := json.Marshal(box)
		path := "/v2/feedback/" + op
		proof, e := relay.Auth(key, "account/"+a.ID, "POST", path, body, "")
		if e != nil {
			t.Fatal(e)
		}
		r := httptest.NewRequest("POST", "https://example.test/agentdeck"+path, bytes.NewReader(body))
		r.Header.Set("Authorization", "Account "+a.ID)
		r.Header.Set("X-Relay-Auth", proof)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("%s status %d != %d", op, w.Code, want)
		}
		return w
	}
	id := "AD-FB-" + strings.Repeat("a", 32)
	v := feedback.Submission{ID: id, Message: "PRIVATE_FEEDBACK_FIXTURE", AppVersion: "0.4.45", Platform: "android"}
	w := call("create", v, 200)
	if mock.calls != 1 || mock.scope != (feedback.Scope{Tenant: a.Identity.Tenant, Owner: a.Identity.Owner}) {
		t.Fatal("scope not derived from account")
	}
	if bytes.Contains(w.Body.Bytes(), []byte(v.Message)) || strings.Contains(logs.String(), v.Message) {
		t.Fatal("feedback leaked")
	}
	var response secure.Box
	if json.Unmarshal(w.Body.Bytes(), &response) != nil {
		t.Fatal("response not encrypted")
	}
	if _, e = secure.Open(key, "account-response/"+a.ID, response); e != nil {
		t.Fatal(e)
	}
	call("get", map[string]string{"id": id}, 200)
	call("list", map[string]int{"limit": 10}, 200)
	mock.fail = true
	call("create", v, 503)
	mock.fail = false
	call("create", map[string]any{"id": id, "message": "manual", "app_version": "v1", "platform": "android", "tenant": "other"}, 400)
	call("create", map[string]any{"id": id, "message": "manual", "app_version": "v1", "platform": "android", "raw_logs": "secret"}, 400)
	v.Local = "foreign"
	db.Put("gwnodes", v.Local, Node{ID: v.Local, Tenant: "tenant-b", Owner: a.Identity.Owner})
	call("create", v, 403)
	foreign := a
	foreign.Identity.Owner = "foreign"
	db.Put("gwaccounts", a.ID, foreign)
	call("get", map[string]string{"id": id}, 404)
	db.Put("gwaccounts", a.ID, a)
	call("delete", map[string]string{"id": id}, 200)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "https://example.test/agentdeck/v2/feedback/create", strings.NewReader(`{}`)))
	if w.Code != 401 {
		t.Fatal("anonymous accepted")
	}
	// Admin status changes are never exposed through the public handler.
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "https://example.test/agentdeck/feedback/status", strings.NewReader(`{}`)))
	if w.Code != 404 {
		t.Fatal("public admin route")
	}
	g.Feedback = nil
	call("list", map[string]int{}, 503)
	g.Feedback = mock
	db.Delete("gwaccounts", a.ID)
	call("get", map[string]string{"id": id}, 401)
}

func TestFeedbackAdminRequiresScope(t *testing.T) {
	g := &Server{Feedback: &feedbackFixture{}}
	m := http.NewServeMux()
	g.feedbackAdminRoutes(m)
	// Exercise the real service's scope rejection rather than trusting request IDs.
	for _, path := range []string{"list", "get", "status", "delete"} {
		r := httptest.NewRequest("POST", "http://unix/feedback/"+path, strings.NewReader(`{ "unexpected": true }`))
		w := httptest.NewRecorder()
		m.ServeHTTP(w, r)
		if w.Code != 400 {
			t.Fatal(path, "allowed unknown admin fields")
		}
		w = httptest.NewRecorder()
		m.ServeHTTP(w, httptest.NewRequest("POST", "http://unix/feedback/"+path, strings.NewReader(`{}`)))
		if w.Code != 400 {
			t.Fatal(path, "accepted missing scope")
		}
	}
}

func TestFeedbackDatabaseGatewayRoundtrip(t *testing.T) {
	for _, driver := range []string{"mysql", "pg", "mongo"} {
		t.Run(driver, func(t *testing.T) {
			dsn := os.Getenv("AGENTDECK_TEST_FEEDBACK_" + strings.ToUpper(driver) + "_DSN")
			if dsn == "" {
				t.Skip("explicit disposable database DSN required")
			}
			service, e := feedback.Open(context.Background(), feedback.Config{Driver: driver, DSN: dsn, Database: os.Getenv("AGENTDECK_TEST_FEEDBACK_MONGO_DATABASE"), Key: secure.Random(32), Retention: 24 * time.Hour})
			if e != nil {
				t.Fatal(e)
			}
			defer service.Close()
			db, e := store.New(t.TempDir())
			if e != nil {
				t.Fatal(e)
			}
			defer db.Close()
			g := New(db, "https://example.test/agentdeck", "", "system", time.Minute)
			g.Feedback = service
			key := secure.Random(32)
			a := Account{ID: secure.ID(), Key: secure.B64.EncodeToString(key), Identity: Identity{Tenant: secure.ID(), Owner: secure.ID()}, Expires: g.Clock.Unix() + 3600}
			if e = db.Put("gwaccounts", a.ID, a); e != nil {
				t.Fatal(e)
			}
			tlsServer := httptest.NewTLSServer(g.Handler())
			defer tlsServer.Close()
			client := tlsServer.Client()
			client.Timeout = 15 * time.Second
			call := func(op string, input any, want int) json.RawMessage {
				t.Helper()
				box, e := secure.SealJSON(key, "account-request/"+a.ID, 0, 0, input)
				if e != nil {
					t.Fatal(e)
				}
				raw, _ := json.Marshal(box)
				path := "/v2/feedback/" + op
				proof, e := relay.Auth(key, "account/"+a.ID, "POST", path, raw, "")
				if e != nil {
					t.Fatal(e)
				}
				r, e := http.NewRequest("POST", tlsServer.URL+"/agentdeck"+path, bytes.NewReader(raw))
				if e != nil {
					t.Fatal(e)
				}
				r.Header.Set("Authorization", "Account "+a.ID)
				r.Header.Set("X-Relay-Auth", proof)
				response, e := client.Do(r)
				if e != nil {
					t.Fatal("TLS feedback request failed")
				}
				defer response.Body.Close()
				body, e := io.ReadAll(io.LimitReader(response.Body, feedback.MaxBody*2))
				if e != nil {
					t.Fatal(e)
				}
				if response.StatusCode != want {
					t.Fatalf("%s: HTTP %d != %d", op, response.StatusCode, want)
				}
				if want != 200 {
					return nil
				}
				var envelope secure.Box
				if json.Unmarshal(body, &envelope) != nil {
					t.Fatal("invalid encrypted result")
				}
				plain, e := secure.Open(key, "account-response/"+a.ID, envelope)
				if e != nil {
					t.Fatal(e)
				}
				var result struct {
					Result json.RawMessage `json:"result"`
				}
				if json.Unmarshal(plain, &result) != nil {
					t.Fatal("bad result")
				}
				return result.Result
			}
			v := feedback.Submission{ID: "AD-FB-" + strings.Repeat("e", 32), Message: "SYNTHETIC_DATABASE_GATEWAY_REPORT", AppVersion: "test", Platform: "android"}
			var receipt struct {
				Created bool `json:"created"`
			}
			if json.Unmarshal(call("create", v, 200), &receipt) != nil || !receipt.Created {
				t.Fatal("create not acknowledged")
			}
			if json.Unmarshal(call("create", v, 200), &receipt) != nil || receipt.Created {
				t.Fatal("retry inserted twice")
			}
			var report feedback.Report
			if json.Unmarshal(call("get", map[string]string{"id": v.ID}, 200), &report) != nil || report.Message != v.Message {
				t.Fatal("database gateway get")
			}
			admin := http.NewServeMux()
			g.feedbackAdminRoutes(admin)
			body, _ := json.Marshal(map[string]string{"tenant": a.Identity.Tenant, "owner": a.Identity.Owner, "id": v.ID, "status": "resolved"})
			w := httptest.NewRecorder()
			admin.ServeHTTP(w, httptest.NewRequest("POST", "http://unix/feedback/status", bytes.NewReader(body)))
			if w.Code != 200 {
				t.Fatal("admin status", w.Code)
			}
			if json.Unmarshal(call("get", map[string]string{"id": v.ID}, 200), &report) != nil || report.Status != "resolved" {
				t.Fatal("status not persisted")
			}
			call("list", map[string]int{"limit": 20}, 200)
			call("delete", map[string]string{"id": v.ID}, 200)
			call("get", map[string]string{"id": v.ID}, 404)
		})
	}
}
