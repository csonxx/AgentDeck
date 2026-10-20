package gateway

import (
	"context"
	"errors"
	"io"
	"net/http"
	"time"

	"agentdeck/internal/feedback"
	"agentdeck/internal/secure"
)

type FeedbackService interface {
	Create(context.Context, feedback.Scope, feedback.Submission) (feedback.Report, bool, error)
	Get(context.Context, feedback.Scope, string) (feedback.Report, error)
	List(context.Context, feedback.Scope, string, int) (feedback.Page, error)
	Delete(context.Context, feedback.Scope, string) error
	SetStatus(context.Context, feedback.Scope, string, string) error
	Prune(context.Context) error
}

func (g *Server) feedbackRoutes(m *http.ServeMux) {
	for _, op := range []string{"create", "list", "get", "delete"} {
		m.HandleFunc("POST /v2/feedback/"+op, g.feedbackRequest(op))
	}
}
func feedbackFail(w http.ResponseWriter, r *http.Request, e error) {
	code, reason := 503, "feedback_storage_unavailable"
	switch {
	case errors.Is(e, feedback.ErrInvalid):
		code, reason = 400, "feedback_invalid"
	case errors.Is(e, feedback.ErrNotFound):
		code, reason = 404, "feedback_not_found"
	case errors.Is(e, feedback.ErrConflict):
		code, reason = 409, "feedback_conflict"
	}
	reject(w, r, code, reason)
}
func (g *Server) feedbackRequest(op string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		a, raw, ok := g.accountAuthLimit(w, r, feedback.MaxBody)
		if !ok {
			return
		}
		if g.Feedback == nil {
			reject(w, r, 503, "feedback_not_configured")
			return
		}
		if a.Identity.Tenant == "" || a.Identity.Owner == "" {
			fail(w, 401)
			return
		}
		key, e := secure.B64.DecodeString(a.Key)
		var box secure.Box
		if e != nil || feedback.Decode(raw, &box) != nil {
			feedbackFail(w, r, feedback.ErrInvalid)
			return
		}
		plain, e := secure.Open(key, "account-request/"+a.ID, box)
		if e != nil {
			feedbackFail(w, r, feedback.ErrInvalid)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		scope := feedback.Scope{Tenant: a.Identity.Tenant, Owner: a.Identity.Owner}
		var result any
		if op == "create" {
			var submission feedback.Submission
			if feedback.Decode(plain, &submission) != nil || submission.Validate() != nil {
				feedbackFail(w, r, feedback.ErrInvalid)
				return
			}
			if submission.Local != "" {
				var node Node
				if g.Store.Get("gwnodes", submission.Local, &node) != nil || node.Tenant != scope.Tenant || node.Owner != scope.Owner {
					reject(w, r, 403, "feedback_workstation_ownership_mismatch")
					return
				}
			}
			var report feedback.Report
			var created bool
			report, created, e = g.Feedback.Create(ctx, scope, submission)
			result = map[string]any{"report": report, "created": created}
		} else if op == "list" {
			var req struct {
				Next  string `json:"next"`
				Limit int    `json:"limit"`
			}
			if feedback.Decode(plain, &req) != nil {
				feedbackFail(w, r, feedback.ErrInvalid)
				return
			}
			if req.Limit == 0 {
				req.Limit = 20
			}
			result, e = g.Feedback.List(ctx, scope, req.Next, req.Limit)
		} else {
			var req struct {
				ID string `json:"id"`
			}
			if feedback.Decode(plain, &req) != nil || !feedback.ValidID(req.ID) {
				feedbackFail(w, r, feedback.ErrInvalid)
				return
			}
			if op == "get" {
				result, e = g.Feedback.Get(ctx, scope, req.ID)
			} else {
				e = g.Feedback.Delete(ctx, scope, req.ID)
				result = map[string]bool{"ok": true}
			}
		}
		if e != nil {
			feedbackFail(w, r, e)
			return
		}
		g.accountWrite(w, a, r, result)
		diagnostic(r, "feedback_operation_completed", map[string]any{"operation": op})
	}
}

// This handler is mounted only on the filesystem-protected owner Unix socket.
func (g *Server) feedbackAdminRoutes(m *http.ServeMux) {
	for _, op := range []string{"list", "get", "status", "delete"} {
		m.HandleFunc("POST /feedback/"+op, func(w http.ResponseWriter, r *http.Request) {
			if g.Feedback == nil {
				fail(w, 503)
				return
			}
			var req struct {
				Tenant string `json:"tenant"`
				Owner  string `json:"owner"`
				ID     string `json:"id"`
				Status string `json:"status"`
				Next   string `json:"next"`
				Limit  int    `json:"limit"`
			}
			raw, e := io.ReadAll(http.MaxBytesReader(w, r.Body, 4<<10))
			if e != nil || feedback.Decode(raw, &req) != nil {
				fail(w, 400)
				return
			}
			if req.Tenant == "" || req.Owner == "" || len(req.Tenant) > 256 || len(req.Owner) > 256 {
				fail(w, 400)
				return
			}
			scope := feedback.Scope{Tenant: req.Tenant, Owner: req.Owner}
			ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
			defer cancel()
			var result any
			switch op {
			case "list":
				if req.Limit == 0 {
					req.Limit = 20
				}
				result, e = g.Feedback.List(ctx, scope, req.Next, req.Limit)
			case "get":
				result, e = g.Feedback.Get(ctx, scope, req.ID)
			case "status":
				e = g.Feedback.SetStatus(ctx, scope, req.ID, req.Status)
				result = map[string]bool{"ok": true}
			case "delete":
				e = g.Feedback.Delete(ctx, scope, req.ID)
				result = map[string]bool{"ok": true}
			}
			if e != nil {
				feedbackFail(w, r, e)
				return
			}
			write(w, result)
		})
	}
}
func (g *Server) PruneFeedback(ctx context.Context) {
	if g.Feedback == nil {
		return
	}
	tick := time.NewTicker(5 * time.Minute)
	defer tick.Stop()
	for {
		op, cancel := context.WithTimeout(ctx, 10*time.Second)
		e := g.Feedback.Prune(op)
		cancel()
		if e != nil && ctx.Err() == nil {
			g.Logger.Warn("feedback_retention_cleanup_failed")
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}
