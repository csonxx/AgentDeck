// Package secure defines the versioned Go/Dart wire protocol. No agent credential
// is used for pairing; each phone gets an independent X25519-derived root.
package secure

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
)

var B64 = base64.StdEncoding

func Random(n int) []byte {
	b := make([]byte, n)
	if _, e := rand.Read(b); e != nil {
		panic(e)
	}
	return b
}
func ID() string { return base64.RawURLEncoding.EncodeToString(Random(18)) }
func Derive(key, salt []byte, info string) []byte {
	k, e := hkdf.Key(sha256.New, key, salt, info, 32)
	if e != nil {
		panic(e)
	}
	return k
}
func PairRoot(private *ecdh.PrivateKey, public, token []byte) ([]byte, error) {
	p, e := ecdh.X25519().NewPublicKey(public)
	if e != nil {
		return nil, e
	}
	s, e := private.ECDH(p)
	if e != nil {
		return nil, e
	}
	return Derive(s, token, "agentdeck/pair/v1"), nil
}
func Traffic(root, salt []byte, direction string, epoch int64) []byte {
	return Derive(root, salt, fmt.Sprintf("agentdeck/traffic/v1/%s/%d", direction, epoch))
}

type Box struct {
	Epoch int64  `json:"epoch"`
	Seq   uint64 `json:"seq"`
	Nonce string `json:"nonce"`
	Data  string `json:"data"`
}

func aad(scope string, epoch int64, seq uint64) []byte {
	return []byte(fmt.Sprintf("agentdeck/v1/%s/%d/%d", scope, epoch, seq))
}
func gcm(key []byte) (cipher.AEAD, error) {
	b, e := aes.NewCipher(key)
	if e != nil {
		return nil, e
	}
	return cipher.NewGCM(b)
}
func Seal(key []byte, scope string, epoch int64, seq uint64, plain []byte) (Box, error) {
	a, e := gcm(key)
	if e != nil {
		return Box{}, e
	}
	n := Random(a.NonceSize())
	return Box{epoch, seq, B64.EncodeToString(n), B64.EncodeToString(a.Seal(nil, n, plain, aad(scope, epoch, seq)))}, nil
}
func Open(key []byte, scope string, b Box) ([]byte, error) {
	a, e := gcm(key)
	if e != nil {
		return nil, e
	}
	n, e := B64.DecodeString(b.Nonce)
	if e != nil || len(n) != a.NonceSize() {
		return nil, errors.New("invalid nonce")
	}
	d, e := B64.DecodeString(b.Data)
	if e != nil {
		return nil, e
	}
	return a.Open(nil, n, d, aad(scope, b.Epoch, b.Seq))
}
func SealJSON(key []byte, scope string, epoch int64, seq uint64, v any) (Box, error) {
	d, e := json.Marshal(v)
	if e != nil {
		return Box{}, e
	}
	return Seal(key, scope, epoch, seq, d)
}
