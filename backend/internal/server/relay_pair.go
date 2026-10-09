package server

import (
	"crypto/ecdh"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"

	"agentdeck/internal/relay"
	"agentdeck/internal/secure"
)

type PendingPair struct {
	Grant    string `json:"grant"`
	Invite   string `json:"invite"`
	Code     string `json:"code"`
	Expires  int64  `json:"expires"`
	Approved bool   `json:"approved"`
}

// SAS binds both ephemeral keys and the derived root. Compare its 48 bits on
// trusted Local/App displays once; Gateway login alone cannot stop an active MITM.
func PairCode(root []byte, invite string) string {
	return hex.EncodeToString(secure.Derive(root, []byte(invite), "agentdeck/pair-sas/v1")[:6])
}
func (s *Server) offer(w http.ResponseWriter, r *http.Request) {
	clock := s.protocolClock(r)
	grant := relay.ContextGrant(r.Context())
	if grant == "" || !s.limit() {
		fail(w, 401)
		return
	}
	var req struct {
		Public string `json:"public"`
	}
	if decode(w, r, &req) != nil {
		fail(w, 400)
		return
	}
	pub, e := secure.B64.DecodeString(req.Public)
	if e != nil || len(pub) != 32 {
		fail(w, 400)
		return
	}
	s.pairMu.Lock()
	defer s.pairMu.Unlock()
	var pending PendingPair
	var iv Invite
	if s.Store.Get("relaypending", grant, &pending) == nil && pending.Expires >= clock.Unix() {
		if s.Store.Get("invites", pending.Invite, &iv) != nil || subtle.ConstantTimeCompare(iv.AppPublic, pub) != 1 {
			fail(w, 401)
			return
		}
	} else {
		rows, e := s.Store.Values("relaypending")
		if e != nil {
			fail(w, 500)
			return
		}
		count := 0
		for _, raw := range rows {
			var v PendingPair
			_ = json.Unmarshal(raw, &v)
			if v.Expires < clock.Unix() {
				_ = s.Store.Delete("relaypending", v.Grant)
				_ = s.Store.Delete("invites", v.Invite)
			} else {
				count++
			}
		}
		if count >= 32 {
			fail(w, 429)
			return
		}
		p, e := ecdh.X25519().GenerateKey(rand.Reader)
		if e != nil {
			fail(w, 500)
			return
		}
		token := secure.Random(32)
		id := relay.Hash(token)
		root, e := secure.PairRoot(p, pub, token)
		if e != nil {
			fail(w, 400)
			return
		}
		iv = Invite{Token: token, Private: p.Bytes(), Expires: clock.Unix() + 300, Grant: grant, AppPublic: pub}
		pending = PendingPair{Grant: grant, Invite: id, Code: PairCode(root, id), Expires: iv.Expires}
		if s.Store.Put("invites", id, iv) != nil || s.Store.Put("relaypending", grant, pending) != nil {
			fail(w, 500)
			return
		}
	}
	p, e := ecdh.X25519().NewPrivateKey(iv.Private)
	if e != nil {
		fail(w, 500)
		return
	}
	// This token is a public HKDF salt in SAS-confirmed mode; no application
	// secret or private key is sent. The app uses its original ephemeral key.
	write(w, map[string]any{"public": secure.B64.EncodeToString(p.PublicKey().Bytes()), "token": secure.B64.EncodeToString(iv.Token), "invite": pending.Invite, "expires": iv.Expires})
}
func (s *Server) Approve(grant, code string) error {
	clock := s.protocolClock(nil)
	if clock == nil {
		return errors.New("gateway clock unavailable")
	}
	s.pairMu.Lock()
	defer s.pairMu.Unlock()
	var p PendingPair
	var iv Invite
	if s.Store.Get("relaypending", grant, &p) != nil || p.Expires < clock.Unix() || s.Store.Get("invites", p.Invite, &iv) != nil || subtle.ConstantTimeCompare([]byte(code), []byte(p.Code)) != 1 {
		return errors.New("pairing request/code rejected")
	}
	iv.Approved = true
	p.Approved = true
	if s.Store.Put("invites", p.Invite, iv) != nil {
		return errors.New("approval persistence failed")
	}
	return s.Store.Put("relaypending", grant, p)
}
