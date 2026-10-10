package console

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestConsoleBoundariesAndPendingRedaction(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/pair-pending":
			io.WriteString(w, `[{"grant":"test-grant","code":"not-for-browser","invite":"secret-invite","expires":2000000000,"approved":false}]`)
		case "/gateway/status":
			io.WriteString(w, `{"linked":true,"online":true}`)
		case "/pair-approve":
			var v map[string]string
			_ = json.NewDecoder(r.Body).Decode(&v)
			if v["code"] != "matching-code" {
				w.WriteHeader(401)
				return
			}
			io.WriteString(w, `{"ok":true}`)
		default:
			t.Errorf("unexpected owner endpoint %s", r.URL.Path)
		}
	}))
	defer upstream.Close()
	client := upstream.Client()
	client.Transport = &rewriteTransport{base: client.Transport, target: upstream.URL}
	c := New(client, "127.0.0.1:12345")
	request := func(method, path, token, origin, host, remote, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "http://"+host+path, strings.NewReader(body))
		r.RemoteAddr = remote
		r.Host = host
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		c.Handler().ServeHTTP(w, r)
		return w
	}
	for _, v := range []struct {
		path, token, origin, host, remote string
		status                            int
	}{
		{"/api/status", "", "", c.Host, "127.0.0.1:20", 401},
		{"/api/status", c.Token, "http://evil.example", c.Host, "127.0.0.1:20", 403},
		{"/api/status", c.Token, "", "evil.example", "127.0.0.1:20", 403},
		{"/api/status", c.Token, "", c.Host, "192.168.1.2:20", 403},
		{"/api/devices", c.Token, "", c.Host, "127.0.0.1:20", 404},
		{"/api/status", c.Token, "", c.Host, "127.0.0.1:20", 200},
		{"/api/status", c.Token, "http://localhost:12345", "localhost:12345", "127.0.0.1:20", 200},
		{"/api/status", c.Token, "http://localhost:12346", "localhost:12345", "127.0.0.1:20", 403},
	} {
		if w := request("GET", v.path, v.token, v.origin, v.host, v.remote, ""); w.Code != v.status {
			t.Errorf("boundary %s got %d want %d", v.path, w.Code, v.status)
		}
	}
	w := request("GET", "/api/pending", c.Token, "", c.Host, "127.0.0.1:20", "")
	if w.Code != 200 || strings.Contains(w.Body.String(), "not-for-browser") || strings.Contains(w.Body.String(), "secret-invite") {
		t.Fatal("pending response leaked comparison material")
	}
	w = request("POST", "/api/approve", c.Token, "http://"+c.Host, c.Host, "127.0.0.1:20", `{"grant":"test-grant","code":"wrong"}`)
	if w.Code != 401 {
		t.Fatal("wrong code accepted")
	}
	w = request("POST", "/api/approve", c.Token, "http://"+c.Host, c.Host, "127.0.0.1:20", `{"grant":"test-grant","code":"matching-code"}`)
	if w.Code != 200 {
		t.Fatal("valid approval failed")
	}
	w = request("GET", "/", "", "", c.Host, "127.0.0.1:20", "")
	if w.Code != 200 || !strings.Contains(w.Header().Get("Content-Security-Policy"), "frame-ancestors 'none'") || strings.Contains(w.Body.String(), c.Token) {
		t.Fatal("page security headers/capability invalid")
	}
}

type rewriteTransport struct {
	base   http.RoundTripper
	target string
}

func (t *rewriteTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	u, _ := http.NewRequest(r.Method, t.target+r.URL.Path, r.Body)
	u.Header = r.Header
	return t.base.RoundTrip(u.WithContext(r.Context()))
}

func TestPlainAddressBootstrapRejectsCrossOriginAccess(t *testing.T) {
	c := New(nil, "127.0.0.1:8787")
	if c.URL() != "http://127.0.0.1:8787/" {
		t.Fatal("console URL should be directly bookmarkable")
	}
	for _, tc := range []struct {
		name, method, host, origin, site, header, remote string
		status                                           int
	}{
		{"fresh tab", "POST", c.Host, "http://" + c.Host, "same-origin", "1", "127.0.0.1:20", 200},
		{"localhost tab", "POST", "localhost:8787", "http://localhost:8787", "same-origin", "1", "127.0.0.1:20", 200},
		{"form without header", "POST", c.Host, "http://" + c.Host, "same-origin", "", "127.0.0.1:20", 403},
		{"navigation", "GET", c.Host, "", "none", "1", "127.0.0.1:20", 403},
		{"cors preflight", "OPTIONS", c.Host, "http://evil.example", "cross-site", "1", "127.0.0.1:20", 403},
		{"different port", "POST", c.Host, "http://127.0.0.1:8788", "same-site", "1", "127.0.0.1:20", 403},
		{"cross-site", "POST", c.Host, "", "cross-site", "1", "127.0.0.1:20", 403},
		{"cross-site origin", "POST", c.Host, "http://evil.example", "", "1", "127.0.0.1:20", 403},
		{"dns rebinding", "POST", "evil.example:8787", "http://evil.example:8787", "same-origin", "1", "127.0.0.1:20", 403},
		{"remote network", "POST", c.Host, "http://" + c.Host, "same-origin", "1", "192.168.1.2:20", 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(tc.method, "http://"+tc.host+"/api/bootstrap", nil)
			r.Host = tc.host
			r.RemoteAddr = tc.remote
			r.Header.Set("Origin", tc.origin)
			r.Header.Set("Sec-Fetch-Site", tc.site)
			r.Header.Set("X-AgentDeck-Console", tc.header)
			w := httptest.NewRecorder()
			c.Handler().ServeHTTP(w, r)
			if w.Code != tc.status {
				t.Fatalf("got %d want %d", w.Code, tc.status)
			}
			if tc.status == 200 {
				var reply map[string]string
				if json.Unmarshal(w.Body.Bytes(), &reply) != nil || reply["token"] != c.Token || w.Header().Get("Cache-Control") != "no-store" {
					t.Fatal("invalid bootstrap response")
				}
			} else if strings.Contains(w.Body.String(), c.Token) {
				t.Fatal("rejected request leaked capability")
			}
			if w.Header().Get("Access-Control-Allow-Origin") != "" {
				t.Fatal("console must not enable CORS")
			}
		})
	}
	next := New(nil, c.Host)
	if next.Token == c.Token {
		t.Fatal("process capability reused")
	}
}
