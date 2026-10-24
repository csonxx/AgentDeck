package server

import (
	"agentdeck/internal/feedback"
	"agentdeck/internal/store"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"sort"
	"time"
)

// Reports use the workstation's existing encrypted vault. The gateway only
// relays ciphertext and needs no feedback database for this destination.
type localFeedback struct {
	Owner, Fingerprint string
	Report             feedback.Report
}

func (s *Server) feedbackRows() ([]localFeedback, error) {
	rows, err := s.Store.Values("feedback")
	if err != nil {
		return nil, err
	}
	out := []localFeedback{}
	for _, raw := range rows {
		var v localFeedback
		if err = json.Unmarshal(raw, &v); err != nil {
			return nil, err
		}
		if !time.Now().Before(v.Report.Expires) {
			if err = s.Store.Delete("feedback", v.Report.ID); err != nil {
				return nil, err
			}
			continue
		}
		out = append(out, v)
	}
	return out, nil
}

func (s *Server) submitFeedback(owner string, raw json.RawMessage) (any, error) {
	var submission feedback.Submission
	if len(raw) > feedback.MaxBody/2 || feedback.Decode(raw, &submission) != nil || submission.Validate() != nil || submission.Local != "" {
		return nil, errors.New("反馈格式无效")
	}
	s.feedbackMu.Lock()
	defer s.feedbackMu.Unlock()
	var dev store.Device
	if s.Store.Get("devices", owner, &dev) != nil {
		return nil, errors.New("设备配对已被工作站删除，请重新配对")
	}
	canonical, _ := json.Marshal(submission)
	fingerprint := sha256.Sum256(canonical)
	fp := hex.EncodeToString(fingerprint[:])
	ack := map[string]any{"id": submission.ID, "stored": "workstation", "diagnostics": len(submission.Diagnostics)}
	var previous localFeedback
	if s.Store.Get("feedback", submission.ID, &previous) == nil {
		if previous.Owner != owner || previous.Fingerprint != fp {
			return nil, errors.New("反馈编号已被其他内容使用")
		}
		return ack, nil
	}
	rows, err := s.feedbackRows()
	if err != nil {
		return nil, errors.New("工作站反馈读取失败")
	}
	count := 0
	for _, v := range rows {
		if v.Owner == owner {
			count++
		}
	}
	if count >= 20 {
		return nil, errors.New("此设备的反馈记录已满，请先由工作站管理员清理")
	}
	now := time.Now().UTC()
	v := localFeedback{Owner: owner, Fingerprint: fp, Report: feedback.Report{Submission: submission, Status: "new", Created: now, Expires: now.Add(30 * 24 * time.Hour)}}
	if s.Store.Put("feedback", submission.ID, v) != nil {
		return nil, errors.New("工作站反馈保存失败")
	}
	slog.Info("feedback_stored", "id", submission.ID, "diagnostics", len(submission.Diagnostics), "destination", "workstation")
	return ack, nil
}

// Mounted exclusively on the filesystem-protected owner socket, never public
// HTTP or the console proxy. Public paired clients may only submit reports.
func (s *Server) feedbackAdminRoutes(m *http.ServeMux) {
	m.HandleFunc("GET /feedback", func(w http.ResponseWriter, r *http.Request) {
		s.feedbackMu.Lock()
		defer s.feedbackMu.Unlock()
		rows, err := s.feedbackRows()
		if err != nil {
			fail(w, 500)
			return
		}
		sort.Slice(rows, func(i, j int) bool { return rows[i].Report.Created.After(rows[j].Report.Created) })
		items := []map[string]any{}
		for _, v := range rows {
			items = append(items, map[string]any{"id": v.Report.ID, "created": v.Report.Created, "expires": v.Report.Expires, "diagnostics": len(v.Report.Diagnostics), "app_version": v.Report.AppVersion, "platform": v.Report.Platform})
		}
		write(w, map[string]any{"items": items})
	})
	m.HandleFunc("GET /feedback/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if !feedback.ValidID(id) {
			fail(w, 400)
			return
		}
		s.feedbackMu.Lock()
		defer s.feedbackMu.Unlock()
		var v localFeedback
		if s.Store.Get("feedback", id, &v) != nil || !time.Now().Before(v.Report.Expires) {
			fail(w, 404)
			return
		}
		write(w, v.Report)
	})
	m.HandleFunc("DELETE /feedback/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if !feedback.ValidID(id) {
			fail(w, 400)
			return
		}
		s.feedbackMu.Lock()
		defer s.feedbackMu.Unlock()
		if s.Store.Delete("feedback", id) != nil {
			fail(w, 500)
			return
		}
		write(w, map[string]bool{"ok": true})
	})
}
