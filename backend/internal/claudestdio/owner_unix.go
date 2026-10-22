//go:build !windows

package claudestdio

import (
	"os"
	"syscall"
)

func owned(s os.FileInfo) bool {
	st, ok := s.Sys().(*syscall.Stat_t)
	return ok && st.Uid == uint32(os.Getuid())
}
