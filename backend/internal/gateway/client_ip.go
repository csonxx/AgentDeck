package gateway

import (
	"errors"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

func (g *Server) ConfigureProxyTrust(value string) error {
	for _, raw := range strings.Split(value, ",") {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		prefix, err := netip.ParsePrefix(strings.TrimSpace(raw))
		if err != nil {
			return errors.New("invalid trusted proxy CIDR configuration")
		}
		g.TrustedProxies = append(g.TrustedProxies, prefix)
	}
	return nil
}
func (g *Server) clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	peer, err := netip.ParseAddr(host)
	if err != nil {
		return ""
	}
	peer = peer.Unmap()
	trusted := func(ip netip.Addr) bool {
		for _, prefix := range g.TrustedProxies {
			if prefix.Contains(ip) {
				return true
			}
		}
		return false
	}
	if !trusted(peer) {
		return peer.String()
	}
	parts := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
	if len(parts) > 16 {
		return peer.String()
	}
	for i := len(parts) - 1; i >= 0; i-- {
		ip, err := netip.ParseAddr(strings.TrimSpace(parts[i]))
		if err != nil {
			return peer.String()
		}
		ip = ip.Unmap()
		if !trusted(ip) || i == 0 {
			return ip.String()
		}
	}
	return peer.String()
}
