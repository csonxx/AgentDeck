//go:build !darwin

package agent

import (
	"context"
	"errors"
)

func openCodexThread(context.Context, string) error {
	return errors.New("native Codex chat wake is supported on macOS")
}
