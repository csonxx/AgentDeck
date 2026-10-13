package agent

import (
	"context"
	"os/exec"
	"time"
)

func openCodexThread(ctx context.Context, id string) error {
	life, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	// Fixed native scheme and validated saved UUID; no shell, prompt, query,
	// arbitrary URL or app-server RPC can be supplied by the phone.
	return exec.CommandContext(life, "/usr/bin/open", "-g", "codex://threads/"+id).Run()
}
