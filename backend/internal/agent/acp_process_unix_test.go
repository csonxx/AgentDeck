//go:build !windows

package agent

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestACPShutdownAlsoStopsOwnedChildProcesses(t *testing.T) {
	m := connectorManager(t)
	fixture, _ := filepath.Abs("../../../scripts/fixture_acp.py")
	childFile := filepath.Join(t.TempDir(), "child.pid")
	c := newACP(m, "qoder", ACPConfig{Command: "python3", Args: []string{fixture, "--child-pid", childFile}})
	m.Connectors = map[string]Connector{"qoder": c}
	_, e := m.Create("qoder", m.Projects()[0].ID, "")
	if e != nil {
		t.Fatal(e)
	}
	b, e := os.ReadFile(childFile)
	if e != nil {
		t.Fatal(e)
	}
	pid, e := strconv.Atoi(string(b))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })
	c.Close()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if syscall.Kill(pid, 0) != nil {
			return
		}
		out, _ := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "stat=").Output()
		if strings.HasPrefix(strings.TrimSpace(string(out)), "Z") {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("owned ACP child process survived shutdown")
}
