package gateway

import "testing"

func TestGatewayAdvertisesDirectPolicyToAuthenticatedLocal(t *testing.T) {
	f := setupPolicy(t, "", func(g *Server) {
		if err := g.ConfigureDirect("true", "stun:stun.example.invalid:3478"); err != nil {
			t.Fatal(err)
		}
	})
	v := f.client.DirectConfig()
	if !v.Enabled || len(v.STUN) != 1 || v.STUN[0] != "stun:stun.example.invalid:3478" {
		t.Fatal("gateway policy did not reach Local")
	}
	// Returned slices cannot mutate the live policy used by other devices.
	v.STUN[0] = "changed"
	if f.client.DirectConfig().STUN[0] == "changed" {
		t.Fatal("mutable policy alias")
	}
}
func TestGatewayRejectsInvalidDirectConfiguration(t *testing.T) {
	f := setup(t)
	if f.g.ConfigureDirect("invalid", "") == nil {
		t.Fatal("invalid bool accepted")
	}
	if f.g.ConfigureDirect("true", "turn:example.invalid:3478") == nil {
		t.Fatal("TURN credentials accepted as public discovery config")
	}
}
