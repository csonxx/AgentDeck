package relay

import (
	"errors"
	"strings"

	"github.com/pion/stun/v4"
)

// Only public STUN discovery addresses are distributed. No third-party default,
// TURN password or device root is included in the Gateway's capabilities.
type DirectConfig struct {
	Version int      `json:"version"`
	Enabled bool     `json:"enabled"`
	STUN    []string `json:"stun_servers"`
}

func DefaultDirectConfig() DirectConfig {
	return DirectConfig{Version: 1, Enabled: true, STUN: []string{}}
}

func (v DirectConfig) Validate() error {
	if v.Version != 1 || len(v.STUN) > 4 {
		return errors.New("invalid direct connection configuration")
	}
	for _, raw := range v.STUN {
		if len(raw) > 512 || strings.ContainsAny(raw, "@/#?\r\n\t ") {
			return errors.New("invalid STUN address")
		}
		u, err := stun.ParseURI(raw)
		if err != nil || u.Scheme != stun.SchemeTypeSTUN || u.Port < 1 || u.Port > 65535 {
			return errors.New("use stun:host:port UDP addresses")
		}
	}
	return nil
}
