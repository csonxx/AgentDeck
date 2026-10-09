//go:build !windows

package agent

import (
	"os"
	"syscall"
)

func desktopOwned(info os.FileInfo) bool {
	v, ok := info.Sys().(*syscall.Stat_t)
	return ok && v.Uid == uint32(os.Getuid())
}
