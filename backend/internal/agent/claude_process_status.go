package agent

import (
	"agentdeck/internal/store"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type claudeProcessRecord struct {
	PID             int    `json:"pid"`
	Session         string `json:"sessionId"`
	Start           string `json:"procStart"`
	Status          string `json:"status"`
	At              int64  `json:"statusUpdatedAt"`
	Parked          string `json:"parkedJobId"`
	ProjectDir      string `json:"cwd"`
	EntryPoint      string `json:"entrypoint"`
	BridgeSessionID string `json:"bridgeSessionId"`
}

// Match both PID and process birth time: stale registry files and PID reuse
// must never report an unrelated process as the current conversation owner.
func claudeProcessSnapshot() (map[int]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "ps", "-axo", "pid=,lstart=,comm=")
	cmd.Env = append(os.Environ(), "TZ=UTC", "LC_ALL=C")
	b, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	out := map[int]string{}
	for _, line := range strings.Split(string(b), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 7 || !strings.Contains(strings.ToLower(strings.Join(fields[6:], " ")), "claude") {
			continue
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}
		out[pid] = strings.Join(fields[1:6], " ")
	}
	return out, nil
}

func nativeClaudeStatus(status string) string {
	switch status {
	case "idle":
		return "idle"
	case "busy":
		return "running"
	case "waiting":
		return "waiting"
	}
	return ""
}

func (f *Follower) refreshClaudeProcessStatuses() {
	if f.claude == nil || !f.claude.ready {
		return
	}
	root := f.ClaudeProcesses
	if root == "" {
		for _, r := range f.Roots {
			if r.Agent == "claude" {
				root = filepath.Join(filepath.Dir(filepath.Clean(r.Path)), "sessions")
				break
			}
		}
	}
	if root == "" {
		return
	}
	if info, err := os.Stat(root); err != nil || !info.IsDir() {
		return // An unavailable registry is not proof of an absent executor.
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	files := make([]string, 0, len(entries))
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".json") {
			files = append(files, filepath.Join(root, entry.Name()))
		}
	}
	snapshot := f.ClaudeProcessSnapshot
	if snapshot == nil {
		snapshot = claudeProcessSnapshot
	}
	processes, err := snapshot()
	if err != nil {
		return
	} // retain evidence, not invented idle
	c := f.claude
	if c.processSeen == nil {
		c.processSeen = map[string]string{}
		c.processOwners = map[string]claudeProcessRecord{}
	}
	owners := map[string]claudeProcessRecord{}
	uncertain := false
	for _, path := range files {
		pid, _ := strconv.Atoi(strings.TrimSuffix(filepath.Base(path), ".json"))
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() || info.Size() > 64<<10 {
			uncertain = uncertain || processes[pid] != ""
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			uncertain = uncertain || processes[pid] != ""
			continue
		}
		var record claudeProcessRecord
		if json.Unmarshal(data, &record) != nil {
			uncertain = uncertain || processes[pid] != ""
			// A partial atomic replacement must not be mistaken for a process exit.
			for id, prior := range c.processOwners {
				if filepath.Base(path) == fmt.Sprint(prior.PID)+".json" && processes[prior.PID] == strings.Join(strings.Fields(prior.Start), " ") {
					owners[id] = prior
				}
			}
			continue
		}
		if record.PID <= 0 || filepath.Base(path) != fmt.Sprint(record.PID)+".json" || record.Parked != "" || record.At <= 0 || nativeClaudeStatus(record.Status) == "" {
			if record.Parked == "" && processes[pid] != "" {
				uncertain = true
			}
			continue
		}
		entry, exists := c.entries[record.Session]
		if !exists || entry.CatalogHidden {
			continue
		}
		if record.Start == "" || processes[record.PID] != strings.Join(strings.Fields(record.Start), " ") {
			continue
		}
		// Prefer the newest confirmed owner if multiple records share one ID.
		if prior, exists := owners[record.Session]; !exists || record.At > prior.At {
			owners[record.Session] = record
		}
	}
	for id, record := range owners {
		stamp := fmt.Sprint(record.PID, "/", record.Start, "/", record.Status, "/", record.At)
		if c.processSeen[id] == stamp {
			continue
		}
		at := time.UnixMilli(record.At).UTC().Format(time.RFC3339Nano)
		if err = f.Normalizer.Store.ObserveClaudeStatus(id, nativeClaudeStatus(record.Status), at); err == nil {
			c.processSeen[id] = stamp
		}
	}
	for id, prior := range c.processOwners {
		if _, exists := owners[id]; exists {
			continue
		}
		if processes[prior.PID] == strings.Join(strings.Fields(prior.Start), " ") {
			owners[id] = prior
			continue
		}
		if prior.Status != "busy" && prior.Status != "waiting" {
			continue
		}
		var current store.Session
		if f.Normalizer.Store.Get("sessions", id, &current) != nil || (current.Status != "running" && current.Status != "waiting" && current.Status != "pending") {
			continue
		}
		// A previously verified owner exited while active. This is interruption,
		// never a fabricated successful completion based on an inactivity timeout.
		if err = f.Normalizer.Store.ObserveClaudeStatus(id, "interrupted", time.Now().UTC().Format(time.RFC3339Nano)); err == nil {
			delete(c.processSeen, id)
		}
	}
	c.processOwners = owners
	if c.orphanSeen == nil {
		c.orphanSeen = map[string]string{}
	}
	for id, stamp := range c.runningTails {
		if _, live := owners[id]; live {
			delete(c.orphanSeen, id)
			continue
		}
		entry, known := c.entries[id]
		if uncertain || !known || entry.CatalogHidden || c.orphanSeen[id] == stamp {
			continue
		}
		var current store.Session
		if err := f.Normalizer.Store.Get("sessions", id, &current); err != nil {
			continue
		}
		if current.Status == "running" || current.Status == "waiting" || current.Status == "pending" {
			// A historical tool call is not a live executor. This also covers
			// owners that exited before this Local started, without guessing
			// completion from the age of the log or changing chat history.
			if err := f.Normalizer.Store.ObserveClaudeStatus(id, "interrupted", time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
				continue
			}
		}
		c.orphanSeen[id] = stamp
	}
}
