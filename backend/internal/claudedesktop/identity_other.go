//go:build !darwin

package claudedesktop

import "context"

type debuggerProcess struct {
	PID   int
	Birth string
}

func debuggerIdentity(context.Context) (debuggerProcess, error) {
	return debuggerProcess{}, ErrUnavailable
}
