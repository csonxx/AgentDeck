package relay

import "testing"

func TestDirectPolicyAddressesNeverInstallThirdPartyDefaults(t *testing.T) {
	v := DefaultDirectConfig()
	if !v.Enabled || len(v.STUN) != 0 || v.Validate() != nil {
		t.Fatal("host-only default changed")
	}
	for _, raw := range []string{"https://example.invalid", "turn:example.invalid:3478", "stun:user@host:3478", "stun:host:70000", "stun:host:0", "stun:host:3478?transport=udp"} {
		v.STUN = []string{raw}
		if v.Validate() == nil {
			t.Fatalf("invalid address accepted: %q", raw)
		}
	}
	for _, raw := range []string{"stun:example.invalid:3478", "stun:[2001:db8::1]:3478"} {
		v.STUN = []string{raw}
		if v.Validate() != nil {
			t.Fatalf("valid STUN address rejected: %q", raw)
		}
	}
}
