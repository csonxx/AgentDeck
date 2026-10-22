//go:build !windows

package localipc

import (
	"context"
	"net"
	"os"
	"path/filepath"
)

func Listen(dir, name string) (net.Listener, error) {
	p := filepath.Join(dir, name+".sock")
	// Do not unlink a live owner's socket when a second process starts.
	if c, err := net.Dial("unix", p); err == nil {
		c.Close()
		return nil, os.ErrExist
	}
	_ = os.Remove(p)
	l, err := net.Listen("unix", p)
	if err == nil {
		err = os.Chmod(p, 0600)
		if err != nil {
			l.Close()
		}
	}
	return l, err
}
func Dial(ctx context.Context, dir, name string) (net.Conn, error) {
	return (&net.Dialer{}).DialContext(ctx, "unix", filepath.Join(dir, name+".sock"))
}
func PrivateDir(dir string) error {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	return os.Chmod(dir, 0700)
}
