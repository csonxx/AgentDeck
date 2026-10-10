package relay

import "testing"

func TestGatewayBaseURLValidation(t *testing.T) {
	for _, raw := range []string{"https://example.com", "https://example.com:8443/agentdeck", "https://example.com/team/agent-deck_1"} {
		if !ValidGatewayURL(raw) {
			t.Fatalf("valid base rejected: %s", raw)
		}
	}
	for _, raw := range []string{"http://example.com/agentdeck", "https://user@example.com", "https://example.com/", "https://example.com/a/", "https://example.com/a//b", "https://example.com/a/../b", "https://example.com/%61", "https://example.com/a%2fb", "https://example.com/a?", "https://example.com/a#", "https://example.com/a?q=1"} {
		if ValidGatewayURL(raw) {
			t.Fatalf("ambiguous/insecure base accepted: %s", raw)
		}
	}
}
