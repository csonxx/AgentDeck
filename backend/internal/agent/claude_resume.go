package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const claudeOwnerBusy = "无法安全交接 Claude 原会话执行端，消息未发送；请刷新后重试"

// The CLI resumes the original native transcript; it is not a desktop IPC
// injection. Refuse concurrent owners instead of forking or corrupting history.
func (m *Manager) checkClaudeOwner(id string, ownPID int) error {
	if m.ClaudeOwner != nil {
		return m.ClaudeOwner(id, ownPID)
	}
	// Also detect parked/resumed SDK processes whose argv has no session ID.
	root := m.claudeProcessRoot()
	if root != "" {
		snapshot := m.ClaudeProcessSnapshot
		if snapshot == nil {
			snapshot = claudeProcessSnapshot
		}
		processes, err := snapshot()
		if err != nil {
			return errors.New("无法确认 Claude 会话占用状态，请稍后重试")
		}
		paths, err := filepath.Glob(filepath.Join(root, "*.json"))
		if err != nil {
			return err
		}
		for _, path := range paths {
			st, err := os.Lstat(path)
			if err != nil || !st.Mode().IsRegular() || st.Size() > 64<<10 || !desktopOwned(st) {
				continue
			}
			b, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			var record claudeProcessRecord
			if json.Unmarshal(b, &record) == nil && record.Session == id && record.PID > 0 && record.PID != ownPID && record.Start != "" && processes[record.PID] == strings.Join(strings.Fields(record.Start), " ") {
				return errors.New(claudeOwnerBusy)
			}
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	b, err := exec.CommandContext(ctx, "ps", "-axo", "pid=,args=").Output()
	if err != nil {
		return errors.New("无法确认 Claude 会话占用状态，请在电脑检查后重试")
	}
	return checkClaudeProcesses(string(b), id, ownPID)
}

func checkClaudeProcesses(list, id string, ownPID int) error {
	flag := regexp.MustCompile(`(?:^|\s)--(?:resume|session-id)(?:=|\s+)` + regexp.QuoteMeta(id) + `(?:\s|$)`)
	for _, line := range strings.Split(list, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil || pid == ownPID {
			continue
		}
		args := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), fields[0]))
		if strings.Contains(strings.ToLower(args), "claude") && flag.MatchString(args) && !strings.Contains(args, "--fork-session") {
			return errors.New(claudeOwnerBusy)
		}
	}
	return nil
}
