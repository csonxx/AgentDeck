// Package feedback stores explicitly submitted diagnostic reports separately
// from agent conversations. Only opaque routing hashes and retention times are
// indexed; report content is authenticated ciphertext in every backend.
package feedback

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"strings"
	"time"

	"agentdeck/internal/secure"
)

var (
	ErrNotFound    = errors.New("feedback not found")
	ErrConflict    = errors.New("feedback ID already has different content or revision")
	ErrInvalid     = errors.New("invalid feedback")
	ErrUnavailable = errors.New("feedback storage unavailable")
)

const MaxBody = 1 << 20

type Scope struct{ Tenant, Owner string }
type Diagnostic struct {
	Time       time.Time `json:"time"`
	Component  string    `json:"component"`
	Code       string    `json:"code"`
	RequestID  string    `json:"request_id,omitempty"`
	DurationMS int64     `json:"duration_ms,omitempty"`
	Status     int       `json:"status,omitempty"`
}

// Deliberately no raw logs, chat messages, tool arguments, URLs or attachments.
type Submission struct {
	ID          string       `json:"id"`
	Message     string       `json:"message"`
	AppVersion  string       `json:"app_version"`
	Platform    string       `json:"platform"`
	Local       string       `json:"local,omitempty"`
	Diagnostics []Diagnostic `json:"diagnostics,omitempty"`
}
type Report struct {
	Submission
	Status  string    `json:"status"`
	Created time.Time `json:"created"`
	Expires time.Time `json:"expires"`
}
type Summary struct {
	ID         string    `json:"id"`
	Status     string    `json:"status"`
	AppVersion string    `json:"app_version"`
	Platform   string    `json:"platform"`
	Created    time.Time `json:"created"`
	Expires    time.Time `json:"expires"`
}
type Page struct {
	Items []Summary `json:"items"`
	Next  string    `json:"next,omitempty"`
}
type row struct {
	Scope       string `bson:"scope"`
	ID          string `bson:"id"`
	Fingerprint string `bson:"fingerprint"`
	Created     int64  `bson:"created"`
	Expires     int64  `bson:"expires"`
	Revision    int64  `bson:"revision"`
	Payload     []byte `bson:"payload"`
}
type cursor struct {
	Created int64  `json:"created"`
	ID      string `json:"id"`
}
type repository interface {
	Insert(context.Context, row) (bool, error)
	Get(context.Context, string, string, int64) (row, error)
	List(context.Context, string, cursor, int, int64) ([]row, error)
	Replace(context.Context, row, int64) (bool, error)
	Delete(context.Context, string, string) error
	Prune(context.Context, int64) error
	Close() error
}
type Service struct {
	repo      repository
	key       []byte
	retention time.Duration
	now       func() time.Time
}

var idPattern = regexp.MustCompile(`^AD-FB-[a-f0-9]{32}$`)
var tokenPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]{0,63}$`)
var diagnosticCode = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_.-]{0,63}$`)
var requestIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$`)

func ValidID(id string) bool { return idPattern.MatchString(id) }
func (s Scope) valid() bool {
	return s.Tenant != "" && s.Owner != "" && len(s.Tenant) <= 256 && len(s.Owner) <= 256
}
func (v Submission) Validate() error {
	if !ValidID(v.ID) || strings.TrimSpace(v.Message) == "" || len(v.Message) > 16<<10 || !tokenPattern.MatchString(v.AppVersion) || !tokenPattern.MatchString(v.Platform) || len(v.Local) > 128 || len(v.Diagnostics) > 1000 {
		return ErrInvalid
	}
	for _, d := range v.Diagnostics {
		if d.Time.IsZero() || !diagnosticCode.MatchString(d.Component) || !diagnosticCode.MatchString(d.Code) || d.RequestID != "" && !requestIDPattern.MatchString(d.RequestID) || d.DurationMS < 0 || d.DurationMS > 24*60*60*1000 || d.Status < 0 || d.Status > 599 {
			return ErrInvalid
		}
	}
	raw, e := json.Marshal(v)
	if e != nil || len(raw) > MaxBody/2 {
		return ErrInvalid
	}
	return nil
}
func Decode(raw []byte, target any) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(target) != nil {
		return ErrInvalid
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return ErrInvalid
	}
	return nil
}
func (s *Service) hash(label string, raw []byte) string {
	h := hmac.New(sha256.New, s.key)
	h.Write([]byte(label))
	h.Write([]byte{0})
	h.Write(raw)
	return hex.EncodeToString(h.Sum(nil))
}
func (s *Service) scope(scope Scope) string {
	b, _ := json.Marshal(scope)
	return s.hash("scope/v1", b)
}
func (s *Service) encode(scope string, r Report) ([]byte, error) {
	b, e := secure.SealJSON(s.key, "feedback/v1/"+scope+"/"+r.ID, 0, 0, r)
	if e != nil {
		return nil, e
	}
	return json.Marshal(b)
}
func (s *Service) decode(r row) (Report, error) {
	var box secure.Box
	var report Report
	if json.Unmarshal(r.Payload, &box) != nil {
		return report, ErrUnavailable
	}
	raw, e := secure.Open(s.key, "feedback/v1/"+r.Scope+"/"+r.ID, box)
	if e != nil || json.Unmarshal(raw, &report) != nil || report.ID != r.ID || report.Created.UnixMilli() != r.Created || report.Expires.UnixMilli() != r.Expires {
		return report, ErrUnavailable
	}
	return report, nil
}
func (s *Service) Create(ctx context.Context, scope Scope, v Submission) (Report, bool, error) {
	if !scope.valid() || v.Validate() != nil {
		return Report{}, false, ErrInvalid
	}
	now := s.now().UTC().Truncate(time.Millisecond)
	r := Report{Submission: v, Status: "new", Created: now, Expires: now.Add(s.retention)}
	key := s.scope(scope)
	raw, _ := json.Marshal(v)
	payload, e := s.encode(key, r)
	if e != nil {
		return Report{}, false, ErrUnavailable
	}
	entry := row{Scope: key, ID: v.ID, Fingerprint: s.hash("submission/v1", raw), Created: r.Created.UnixMilli(), Expires: r.Expires.UnixMilli(), Revision: 1, Payload: payload}
	added, e := s.repo.Insert(ctx, entry)
	if e != nil {
		return Report{}, false, ErrUnavailable
	}
	if added {
		return r, true, nil
	}
	old, e := s.repo.Get(ctx, key, v.ID, now.UnixMilli())
	if e != nil {
		return Report{}, false, storageError(e)
	}
	if old.Fingerprint != entry.Fingerprint {
		return Report{}, false, ErrConflict
	}
	r, e = s.decode(old)
	return r, false, e
}
func storageError(e error) error {
	if errors.Is(e, ErrNotFound) {
		return ErrNotFound
	}
	return ErrUnavailable
}
func (s *Service) Get(ctx context.Context, scope Scope, id string) (Report, error) {
	if !scope.valid() || !ValidID(id) {
		return Report{}, ErrInvalid
	}
	r, e := s.repo.Get(ctx, s.scope(scope), id, s.now().UnixMilli())
	if e != nil {
		return Report{}, storageError(e)
	}
	return s.decode(r)
}
func (s *Service) List(ctx context.Context, scope Scope, next string, limit int) (Page, error) {
	if !scope.valid() || limit < 1 || limit > 100 {
		return Page{}, ErrInvalid
	}
	var c cursor
	key := s.scope(scope)
	if next != "" {
		b, e := secure.B64.DecodeString(next)
		var box secure.Box
		if e != nil || len(b) > 2048 || json.Unmarshal(b, &box) != nil {
			return Page{}, ErrInvalid
		}
		plain, e := secure.Open(s.key, "feedback-cursor/v1/"+key, box)
		if e != nil || json.Unmarshal(plain, &c) != nil || !ValidID(c.ID) || c.Created <= 0 {
			return Page{}, ErrInvalid
		}
	}
	rows, e := s.repo.List(ctx, key, c, limit+1, s.now().UnixMilli())
	if e != nil {
		return Page{}, ErrUnavailable
	}
	p := Page{Items: []Summary{}}
	if len(rows) > limit {
		last := rows[limit-1]
		box, e := secure.SealJSON(s.key, "feedback-cursor/v1/"+key, 0, 0, cursor{last.Created, last.ID})
		if e != nil {
			return Page{}, ErrUnavailable
		}
		b, _ := json.Marshal(box)
		p.Next = secure.B64.EncodeToString(b)
		rows = rows[:limit]
	}
	for _, entry := range rows {
		r, e := s.decode(entry)
		if e != nil {
			return Page{}, e
		}
		p.Items = append(p.Items, Summary{r.ID, r.Status, r.AppVersion, r.Platform, r.Created, r.Expires})
	}
	return p, nil
}
func (s *Service) Delete(ctx context.Context, scope Scope, id string) error {
	if !scope.valid() || !ValidID(id) {
		return ErrInvalid
	}
	if e := s.repo.Delete(ctx, s.scope(scope), id); e != nil {
		return ErrUnavailable
	}
	return nil
}
func (s *Service) SetStatus(ctx context.Context, scope Scope, id, status string) error {
	if !scope.valid() || !ValidID(id) || status != "new" && status != "in_progress" && status != "resolved" && status != "closed" {
		return ErrInvalid
	}
	r, e := s.repo.Get(ctx, s.scope(scope), id, s.now().UnixMilli())
	if e != nil {
		return storageError(e)
	}
	v, e := s.decode(r)
	if e != nil {
		return e
	}
	v.Status = status
	r.Payload, e = s.encode(r.Scope, v)
	if e != nil {
		return ErrUnavailable
	}
	old := r.Revision
	r.Revision++
	ok, e := s.repo.Replace(ctx, r, old)
	if e != nil {
		return ErrUnavailable
	}
	if !ok {
		return ErrConflict
	}
	return nil
}
func (s *Service) Prune(ctx context.Context) error {
	if e := s.repo.Prune(ctx, s.now().UnixMilli()); e != nil {
		return ErrUnavailable
	}
	return nil
}
func (s *Service) Close() error { return s.repo.Close() }
