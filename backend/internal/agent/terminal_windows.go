//go:build windows

package agent

import "os/exec"

func terminalProcessGroup(c *exec.Cmd) {}
func killTerminalProcess(c *exec.Cmd) {
	if c.Process != nil {
		_ = c.Process.Kill()
	}
}
