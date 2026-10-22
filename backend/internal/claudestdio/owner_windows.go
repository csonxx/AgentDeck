package claudestdio

import "os"

// This legacy Unix stdin bridge is unavailable on Windows; never weaken its
// ownership check to make the standalone workstation build compile.
func owned(os.FileInfo) bool { return false }
