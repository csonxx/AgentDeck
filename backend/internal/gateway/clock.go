package gateway

import (
	"agentdeck/internal/relay"
	"agentdeck/internal/secure"
	"encoding/json"
	"net/http"
)

func (g *Server) timeSync(w http.ResponseWriter, r *http.Request) {
	var req secure.ClockRequest
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&req) != nil || !relay.ValidID(req.ID) {
		reject(w, r, 400, "clock_request_invalid")
		return
	}
	var key string
	switch req.Kind {
	case "local":
		var n Node
		if g.Store.Get("gwnodes", req.ID, &n) != nil {
			fail(w, 401)
			return
		}
		key = n.Key
	case "grant":
		var grant relay.Grant
		var n Node
		if g.Store.Get("gwgrants", req.ID, &grant) != nil || grant.Device == "" && grant.Expires < g.Clock.Unix() || g.Store.Get("gwnodes", grant.Local, &n) != nil || n.Tenant != grant.Tenant {
			fail(w, 401)
			return
		}
		key = grant.Key
	case "account":
		var a Account
		if g.Store.Get("gwaccounts", req.ID, &a) != nil || a.Expires < g.Clock.Unix() {
			fail(w, 401)
			return
		}
		key = a.Key
	case "flow":
		var flow Flow
		if g.Store.Get("gwflows", req.ID, &flow) != nil || flow.Expires < g.Clock.Unix() {
			fail(w, 401)
			return
		}
		key = flow.Key
	default:
		fail(w, 400)
		return
	}
	secret, err := secure.B64.DecodeString(key)
	if err != nil || len(secret) != 32 {
		fail(w, 401)
		return
	}
	scope := req.Kind + "/" + req.ID
	challenge, err := secure.ClockChallenge(secret, scope, req.Box)
	if err != nil || !g.replay.Accept("clock/"+scope+":"+req.Box.Nonce) {
		reject(w, r, 401, "clock_challenge_or_replay_rejected")
		return
	}
	box, err := secure.ClockReply(secret, scope, challenge, g.Clock)
	if err != nil {
		fail(w, 500)
		return
	}
	write(w, box)
}
