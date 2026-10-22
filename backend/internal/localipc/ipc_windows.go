package localipc

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"

	winio "github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"
)

func ownerSID() (string, error) {
	t, err := windows.OpenCurrentProcessToken()
	if err != nil {
		return "", err
	}
	defer t.Close()
	u, err := t.GetTokenUser()
	if err != nil {
		return "", err
	}
	return u.User.Sid.String(), nil
}
func pipe(dir, name string) string {
	p, _ := filepath.Abs(dir)
	h := sha256.Sum256([]byte(strings.ToLower(filepath.Clean(p)) + "/" + name))
	return fmt.Sprintf(`\\.\pipe\agentdeck-%x`, h[:20])
}
func Listen(dir, name string) (net.Listener, error) {
	sid, err := ownerSID()
	if err != nil {
		return nil, err
	}
	return winio.ListenPipe(pipe(dir, name), &winio.PipeConfig{SecurityDescriptor: "D:P(A;;GA;;;" + sid + ")", InputBufferSize: 65536, OutputBufferSize: 65536})
}
func Dial(ctx context.Context, dir, name string) (net.Conn, error) {
	return winio.DialPipeContext(ctx, pipe(dir, name))
}
func PrivateDir(dir string) error {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	sid, err := ownerSID()
	if err != nil {
		return err
	}
	sd, err := windows.SecurityDescriptorFromString("D:P(A;OICI;FA;;;" + sid + ")")
	if err != nil {
		return err
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return err
	}
	return windows.SetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil)
}
