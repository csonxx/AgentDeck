package agent

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"os"
	"time"

	"agentdeck/internal/store"
	_ "modernc.org/sqlite"
)

// The native goal RPC requires an already loaded thread. Discovery must not
// resume every user's chat: read only the native goal ledger, including WAL.
// Mutations still use the native RPC, never direct SQL writes.
func (m *Manager) codexGoals(sessions []store.Session) ([]Goal, error) {
	result := []Goal{}
	if m.CodexGoalPath == "" {
		return result, nil
	}
	if _, e := os.Stat(m.CodexGoalPath); os.IsNotExist(e) {
		return result, nil
	} else if e != nil {
		return nil, errors.New("目标接口未确认，请刷新后检查状态")
	}
	known := map[string]store.Session{}
	for _, s := range sessions {
		if s.Agent == "codex" {
			known[s.ID] = s
		}
	}
	u := url.URL{Scheme: "file", Path: m.CodexGoalPath}
	u.RawQuery = "mode=ro&_pragma=busy_timeout(1000)"
	db, e := sql.Open("sqlite", u.String())
	if e != nil {
		return nil, errors.New("目标接口未确认，请刷新后检查状态")
	}
	defer db.Close()
	ctx := m.Context
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	rows, e := db.QueryContext(ctx, `SELECT thread_id, objective, status, token_budget, tokens_used, time_used_seconds, created_at_ms, updated_at_ms FROM thread_goals LIMIT 10000`)
	if e != nil {
		return nil, errors.New("目标接口不可用，请更新电脑 Agent 后重试")
	}
	defer rows.Close()
	for rows.Next() {
		var g Goal
		var budget sql.NullInt64
		var created, updated int64
		if e = rows.Scan(&g.Session, &g.Objective, &g.Status, &budget, &g.Tokens, &g.Seconds, &created, &updated); e != nil {
			return nil, errors.New("目标接口未确认，请刷新后检查状态")
		}
		s, ok := known[g.Session]
		if !ok || len(g.Objective) > 16<<10 {
			continue
		}
		g.Agent = "codex"
		g.Created, g.Updated = created/1000, updated/1000
		g.Objective = Redact(g.Objective)
		if budget.Valid {
			v := budget.Int64
			g.Budget = &v
		}
		result = append(result, m.decorateGoal(g, s))
	}
	if rows.Err() != nil {
		return nil, errors.New("目标接口未确认，请刷新后检查状态")
	}
	return result, nil
}
