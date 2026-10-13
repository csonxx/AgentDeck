//go:build darwin

package claudedesktop

import (
	"context"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

type debuggerProcess struct {
	PID   int
	Birth string
}

func debuggerIdentity(ctx context.Context) (debuggerProcess, error) {
	var zero debuggerProcess
	b, err := exec.CommandContext(ctx, "/usr/sbin/lsof", "-nP", "-iTCP:9229", "-sTCP:LISTEN", "-Fpn").Output()
	if err != nil {
		return zero, ErrUnavailable
	}
	var pid int
	addresses := 0
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, "p") {
			n, e := strconv.Atoi(line[1:])
			if e != nil || pid != 0 && pid != n {
				return zero, ErrUnavailable
			}
			pid = n
		}
		if strings.HasPrefix(line, "n") {
			if line != "n127.0.0.1:9229" {
				return zero, ErrUnavailable
			}
			addresses++
		}
	}
	if pid <= 0 || addresses != 1 {
		return zero, ErrUnavailable
	}
	cmd := exec.CommandContext(ctx, "/bin/ps", "-p", strconv.Itoa(pid), "-o", "uid=,lstart=,comm=")
	cmd.Env = append(os.Environ(), "TZ=UTC", "LC_ALL=C")
	b, err = cmd.Output()
	fields := strings.Fields(string(b))
	if err != nil || len(fields) != 7 || fields[0] != strconv.Itoa(os.Getuid()) || fields[6] != "/Applications/Claude.app/Contents/MacOS/Claude" {
		return zero, ErrUnavailable
	}
	return debuggerProcess{PID: pid, Birth: strings.Join(fields[1:6], " ")}, nil
}
