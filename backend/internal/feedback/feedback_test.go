package feedback

import (
	"bytes"
	"context"
	"errors"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"agentdeck/internal/secure"
)

// Memory fixture exercises service encryption independently of database dialects.
type memoryRepository struct {
	mu   sync.Mutex
	rows map[string]row
}

func (m *memoryRepository) Insert(_ context.Context, r row) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	k := r.Scope + r.ID
	if _, ok := m.rows[k]; ok {
		return false, nil
	}
	m.rows[k] = r
	return true, nil
}
func (m *memoryRepository) Get(_ context.Context, s, id string, now int64) (row, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.rows[s+id]
	if !ok || r.Expires <= now {
		return row{}, ErrNotFound
	}
	return r, nil
}
func (m *memoryRepository) List(_ context.Context, s string, c cursor, n int, now int64) ([]row, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []row{}
	for _, r := range m.rows {
		if r.Scope == s && r.Expires > now && (c.ID == "" || r.Created < c.Created || r.Created == c.Created && r.ID < c.ID) {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Created == out[j].Created {
			return out[i].ID > out[j].ID
		}
		return out[i].Created > out[j].Created
	})
	if len(out) > n {
		out = out[:n]
	}
	return out, nil
}
func (m *memoryRepository) Replace(_ context.Context, r row, rev int64) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	old, ok := m.rows[r.Scope+r.ID]
	if !ok || old.Revision != rev {
		return false, nil
	}
	m.rows[r.Scope+r.ID] = r
	return true, nil
}
func (m *memoryRepository) Delete(_ context.Context, s, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.rows, s+id)
	return nil
}
func (m *memoryRepository) Prune(_ context.Context, now int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for k, r := range m.rows {
		if r.Expires <= now {
			delete(m.rows, k)
		}
	}
	return nil
}
func (m *memoryRepository) Close() error { return nil }
func fixtureSubmission(id string) Submission {
	return Submission{ID: "AD-FB-" + strings.Repeat(id, 32), Message: "synthetic feedback marker", AppVersion: "0.4.45", Platform: "android", Diagnostics: []Diagnostic{{Time: time.Now().UTC().Truncate(time.Millisecond), Component: "app", Code: "sync_timeout", RequestID: "fixture-request", DurationMS: 123, Status: 504}}}
}
func contract(t *testing.T, s *Service) (Scope, string) {
	t.Helper()
	ctx := context.Background()
	scope := Scope{"tenant-" + secure.ID(), "owner-" + secure.ID()}
	v := fixtureSubmission("a")
	var added atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, created, e := s.Create(ctx, scope, v)
			if e != nil {
				t.Error(e)
			}
			if created {
				added.Add(1)
			}
		}()
	}
	wg.Wait()
	if added.Load() != 1 {
		t.Fatal("concurrent retries inserted multiple reports", added.Load())
	}
	r, e := s.Get(ctx, scope, v.ID)
	if e != nil || r.Message != v.Message || r.Status != "new" {
		t.Fatal("report roundtrip", e)
	}
	changed := v
	changed.Message = "different"
	if _, _, e = s.Create(ctx, scope, changed); !errors.Is(e, ErrConflict) {
		t.Fatal("ID reuse changed content", e)
	}
	for _, other := range []Scope{{scope.Tenant, "other-owner"}, {"other-tenant", scope.Owner}} {
		if _, e = s.Get(ctx, other, v.ID); !errors.Is(e, ErrNotFound) {
			t.Fatal("scope leaked", e)
		}
	}
	entry, e := s.repo.Get(ctx, s.scope(scope), v.ID, s.now().UnixMilli())
	if e != nil {
		t.Fatal(e)
	}
	if entry.Scope == scope.Tenant || bytes.Contains(entry.Payload, []byte(v.Message)) || bytes.Contains(entry.Payload, []byte("sync_timeout")) {
		t.Fatal("plaintext reached database")
	}
	wrong := *s
	wrong.key = secure.Random(32)
	if _, e = wrong.decode(entry); !errors.Is(e, ErrUnavailable) {
		t.Fatal("wrong key decrypted report")
	}
	if e = s.SetStatus(ctx, scope, v.ID, "in_progress"); e != nil {
		t.Fatal(e)
	}
	r, e = s.Get(ctx, scope, v.ID)
	if e != nil || r.Status != "in_progress" {
		t.Fatal("status update", e)
	}
	stale := entry
	stale.Revision++
	if ok, e := s.repo.Replace(ctx, stale, entry.Revision); e != nil || ok {
		t.Fatal("stale revision overwrote update", e)
	}
	if e = s.SetStatus(ctx, scope, v.ID, "anything"); !errors.Is(e, ErrInvalid) {
		t.Fatal("invalid status accepted")
	}
	for _, id := range []string{"b", "c"} {
		if _, _, e = s.Create(ctx, scope, fixtureSubmission(id)); e != nil {
			t.Fatal(e)
		}
	}
	seen := map[string]bool{}
	next := ""
	for i := 0; i < 4; i++ {
		p, e := s.List(ctx, scope, next, 1)
		if e != nil || len(p.Items) != 1 {
			t.Fatal("pagination", e)
		}
		if seen[p.Items[0].ID] {
			t.Fatal("page duplicated")
		}
		seen[p.Items[0].ID] = true
		next = p.Next
		if next == "" {
			break
		}
		if _, e = s.List(ctx, Scope{scope.Tenant, "foreign"}, next, 1); !errors.Is(e, ErrInvalid) {
			t.Fatal("foreign cursor accepted", e)
		}
	}
	if len(seen) != 3 {
		t.Fatal("page lost reports")
	}
	if e = s.Delete(ctx, scope, "AD-FB-"+strings.Repeat("b", 32)); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Get(ctx, scope, "AD-FB-"+strings.Repeat("b", 32)); !errors.Is(e, ErrNotFound) {
		t.Fatal("deleted row visible")
	}
	return scope, v.ID
}
func TestEncryptedFeedbackContract(t *testing.T) {
	s := &Service{repo: &memoryRepository{rows: map[string]row{}}, key: secure.Random(32), retention: 24 * time.Hour, now: time.Now}
	scope, id := contract(t, s)
	s.now = func() time.Time { return time.Now().Add(25 * time.Hour) }
	if _, e := s.Get(context.Background(), scope, id); !errors.Is(e, ErrNotFound) {
		t.Fatal("expired row visible")
	}
	if e := s.Prune(context.Background()); e != nil {
		t.Fatal(e)
	}
	if len(s.repo.(*memoryRepository).rows) != 0 {
		t.Fatal("expired rows retained")
	}
}
func TestConfiguredDatabaseContract(t *testing.T) {
	for _, driver := range []string{"mysql", "pg", "mongo"} {
		t.Run(driver, func(t *testing.T) {
			dsn := os.Getenv("AGENTDECK_TEST_FEEDBACK_" + strings.ToUpper(driver) + "_DSN")
			if dsn == "" {
				t.Skip("explicit disposable database DSN required")
			}
			c := Config{Driver: driver, DSN: dsn, Database: os.Getenv("AGENTDECK_TEST_FEEDBACK_MONGO_DATABASE"), Key: secure.Random(32), Retention: 24 * time.Hour}
			s, e := Open(context.Background(), c)
			if e != nil {
				t.Fatal(e)
			}
			scope, id := contract(t, s)
			if e = s.Close(); e != nil {
				t.Fatal(e)
			}
			s, e = Open(context.Background(), c)
			if e != nil {
				t.Fatal(e)
			}
			defer s.Close()
			if _, e = s.Get(context.Background(), scope, id); e != nil {
				t.Fatal("restart lost encrypted report", e)
			}
			s.now = func() time.Time { return time.Now().Add(25 * time.Hour) }
			if _, e = s.Get(context.Background(), scope, id); !errors.Is(e, ErrNotFound) {
				t.Fatal("expired row visible")
			}
			if e = s.Prune(context.Background()); e != nil {
				t.Fatal(e)
			}
			if _, e = s.repo.Get(context.Background(), s.scope(scope), id, time.Now().UnixMilli()); !errors.Is(e, ErrNotFound) {
				t.Fatal("retention did not delete", e)
			}
		})
	}
}
func TestFeedbackValidationAndConfig(t *testing.T) {
	key := secure.B64.EncodeToString(secure.Random(32))
	env := map[string]string{"AGENTDECK_FEEDBACK_DRIVER": "postgresql", "AGENTDECK_FEEDBACK_DSN": "postgres://secret-placeholder", "AGENTDECK_FEEDBACK_KEY": key}
	get := func(k string) string { return env[k] }
	c, e := FromEnv(get)
	if e != nil || c.Driver != "pg" {
		t.Fatal(e)
	}
	for _, field := range []string{"AGENTDECK_FEEDBACK_DRIVER", "AGENTDECK_FEEDBACK_KEY", "AGENTDECK_FEEDBACK_RETENTION"} {
		old := env[field]
		env[field] = "invalid-secret-placeholder"
		_, e = FromEnv(get)
		if e == nil || strings.Contains(e.Error(), "placeholder") {
			t.Fatal("invalid or secret config leaked")
		}
		env[field] = old
	}
	if c, e = FromEnv(func(string) string { return "" }); e != nil || c.Driver != "off" {
		t.Fatal("default enabled")
	}
	v := fixtureSubmission("a")
	v.Diagnostics[0].Code = "https://host?token=secret"
	if v.Validate() == nil {
		t.Fatal("raw diagnostic accepted")
	}
	v.Diagnostics[0].Code = "https://host/secret"
	if v.Validate() == nil {
		t.Fatal("URL diagnostic accepted")
	}
	var sub Submission
	if Decode([]byte(`{"id":"x","raw_logs":"secret"}`), &sub) == nil || Decode([]byte(`{} {}`), &sub) == nil {
		t.Fatal("unrecognized or trailing data accepted")
	}
	for _, config := range []Config{{Driver: "mysql", DSN: "u:p@tcp(db.example:3306)/ad?tls=skip-verify"}, {Driver: "pg", DSN: "postgres://u:p@db.example/ad?sslmode=disable"}, {Driver: "mongo", DSN: "mongodb://db.example/?tls=true&tlsInsecure=true", Database: "agentdeck"}} {
		config.Key = secure.Random(32)
		config.Retention = 24 * time.Hour
		if _, e := Open(context.Background(), config); e == nil {
			t.Fatal("unsafe TLS accepted")
		}
	}
}
