package server

import (
	"agentdeck/internal/relay"
	"agentdeck/internal/secure"
	"agentdeck/internal/store"
	"net/http"
	"time"
)

func (s *Server) acceptNonce(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for key, at := range s.replay {
		if now.Sub(at) > 2*time.Minute {
			delete(s.replay, key)
		}
	}
	if _, exists := s.replay[id]; exists || len(s.replay) >= 4096 {
		return false
	}
	s.replay[id] = now
	return true
}
func (s *Server) timeSync(w http.ResponseWriter, r *http.Request) {
	if !s.limit() {
		fail(w, 429)
		return
	}
	var req secure.ClockRequest
	if decode(w, r, &req) != nil || req.Kind != "device" {
		fail(w, 400)
		return
	}
	var dev store.Device
	if s.Store.Get("devices", req.ID, &dev) != nil || dev.RelayGrant != relay.ContextGrant(r.Context()) {
		fail(w, 401)
		return
	}
	scope := "device/" + dev.ID
	challenge, err := secure.ClockChallenge(dev.Root, scope, req.Box)
	if err != nil || !s.acceptNonce("clock/"+scope+":"+req.Box.Nonce) {
		fail(w, 401)
		return
	}
	box, err := secure.ClockReply(dev.Root, scope, challenge, s.protocolClock(r))
	if err != nil {
		fail(w, 500)
		return
	}
	write(w, box)
}

func (s *Server) protocolClock(r *http.Request) *secure.Clock {
	if r != nil {
		if c := relay.ContextClock(r.Context()); c != nil {
			return c
		}
	}
	if s.RelayClock != nil {
		return s.RelayClock()
	}
	return s.Clock
}
