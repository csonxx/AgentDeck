package gateway

import (
	"agentdeck/internal/relay"
	"errors"
	"strconv"
	"strings"
)

// ConfigureDirect runs at startup. Empty STUN means host/IPv6 candidates only;
// Gateway remains a signaling/authorization service, never a UDP STUN listener.
func (g *Server) ConfigureDirect(enabled, servers string) error {
	policy := relay.DefaultDirectConfig()
	if enabled != "" {
		v, err := strconv.ParseBool(enabled)
		if err != nil {
			return errors.New("AGENTDECK_P2P_ENABLED must be true or false")
		}
		policy.Enabled = v
	}
	if strings.TrimSpace(servers) != "" {
		for _, s := range strings.Split(servers, ",") {
			policy.STUN = append(policy.STUN, strings.TrimSpace(s))
		}
	}
	if err := policy.Validate(); err != nil {
		return err
	}
	g.Direct = policy
	g.Logger.Info("direct_connection_policy", "enabled", policy.Enabled, "stun_servers", len(policy.STUN))
	return nil
}
