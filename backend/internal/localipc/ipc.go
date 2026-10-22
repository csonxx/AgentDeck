// Package localipc keeps administrative HTTP off public listeners. Unix uses
// an owner-only socket; Windows uses a pipe restricted to the current user SID.
package localipc

import (
	"context"
	"net"
	"net/http"
	"time"
)

func Client(dir, name string) *http.Client {
	return &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return Dial(ctx, dir, name)
	}}}
}
