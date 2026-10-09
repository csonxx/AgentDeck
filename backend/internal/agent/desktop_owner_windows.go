package agent

import "os"

func desktopOwned(os.FileInfo) bool { return false }
