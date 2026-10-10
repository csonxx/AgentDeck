package server

import (
	"context"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"agentdeck/internal/gateway"
	"agentdeck/internal/relay"
	"agentdeck/internal/store"
)

func waitAssociation(t *testing.T, ready func() bool) {
	t.Helper()
	until := time.Now().Add(8 * time.Second)
	for !ready() {
		if time.Now().After(until) {
			t.Fatal("association transition timed out")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestOwnerPageAssociationFromUnlinkedToSaved(t *testing.T) {
	gwdb, e := store.New(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer gwdb.Close()
	g := gateway.New(gwdb, "", "", "system", time.Second)
	ts := httptest.NewTLSServer(g.Handler())
	defer ts.Close()
	defer g.Close()
	g.URL = ts.URL + "/agentdeck"
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			io.WriteString(w, `{"code":0,"access_token":"synthetic-token"}`)
		} else {
			io.WriteString(w, `{"code":0,"data":{"tenant_key":"test-tenant","open_id":"test-owner","name":"test"}}`)
		}
	}))
	defer provider.Close()
	g.Feishu = gateway.NewFeishu("test-id", "test-secret", g.URL+"/v2/auth/callback")
	g.Feishu.TokenURL = provider.URL + "/token"
	g.Feishu.UserURL = provider.URL + "/user"
	db, e := store.New(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	s := New(db, "", "https://localhost:8443", time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if e := s.EnableRelay(ctx); e != nil {
		t.Fatal(e)
	}
	defer func() { cancel(); s.Association.Wait() }()
	a := s.Association
	a.loginHTTP = ts.Client()
	if a.Status().Linked || a.Status().State != "unlinked" {
		t.Fatal("fresh directory incorrectly linked")
	}
	if e := a.Begin("http://unsafe.example", "test"); e == nil {
		t.Fatal("plain HTTP enrollment accepted")
	}
	if e := a.Begin(g.URL, "Browser workstation"); e != nil {
		t.Fatal(e)
	}
	if e := a.Begin(g.URL, "duplicate"); e == nil {
		t.Fatal("concurrent enrollment accepted")
	}
	waitAssociation(t, func() bool { return a.Status().LoginURL != "" })
	flowURL, e := url.Parse(a.Status().LoginURL)
	if e != nil {
		t.Fatal(e)
	}
	jar, _ := cookiejar.New(nil)
	browser := &http.Client{Transport: ts.Client().Transport, Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, e := browser.Get(g.URL + "/v2/auth/authorize?flow=" + flowURL.Query().Get("flow"))
	if e != nil {
		t.Fatal(e)
	}
	res.Body.Close()
	redirect, _ := url.Parse(res.Header.Get("Location"))
	res, e = browser.Get(g.URL + "/v2/auth/callback?state=" + redirect.Query().Get("state") + "&code=synthetic-code")
	if e != nil {
		t.Fatal(e)
	}
	res.Body.Close()
	if res.StatusCode != 303 {
		t.Fatal("fixture callback rejected")
	}
	waitAssociation(t, func() bool { return a.Status().Linked })
	var saved relay.Link
	if e := db.Get("relayconfig", "gateway", &saved); e != nil || saved.URL != g.URL || saved.Tenant != "test-tenant" || saved.Validate() != nil {
		t.Fatal("verified enrollment not saved")
	}
	if a.Status().LoginURL != "" || a.Status().Name != "Browser workstation" {
		t.Fatal("completed flow not cleared")
	}
}

func TestAssociationCancellationAndPublicRoutes(t *testing.T) {
	db, e := store.New(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	s := New(db, "", "https://localhost:8443", time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if e := s.EnableRelay(ctx); e != nil {
		t.Fatal(e)
	}
	defer func() { cancel(); s.Association.Wait() }()
	for _, path := range []string{"/gateway/status", "/gateway/login", "/pair-approve"} {
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code == 200 {
			t.Fatal("owner operation exposed on phone handler")
		}
	}
	slow := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer slow.Close()
	s.Association.loginHTTP = slow.Client()
	if e := s.Association.Begin(slow.URL, "Cancel test"); e != nil {
		t.Fatal(e)
	}
	s.Association.Cancel()
	waitAssociation(t, func() bool { return s.Association.Status().State == "failed" })
	if s.Association.Status().Linked {
		t.Fatal("cancelled flow created an association")
	}
}
