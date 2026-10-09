package secure

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/json"
	"os"
	"testing"
)

func TestAuthenticatedEncryptionAndDirections(t *testing.T) {
	root := make([]byte, 32)
	salt := make([]byte, 32)
	for i := range root {
		root[i] = byte(i)
		salt[i] = byte(i + 32)
	}
	key := Traffic(root, salt, "s2c", 17)
	b, e := SealJSON(key, "s2c", 17, 3, map[string]any{"message": "你好 AgentDeck", "nested": []int{1, 2}})
	if e != nil {
		t.Fatal(e)
	}
	p, e := Open(key, "s2c", b)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = Open(Traffic(root, salt, "c2s", 17), "s2c", b); e == nil {
		t.Fatal("direction reflection accepted")
	}
	b.Seq++
	if _, e = Open(key, "s2c", b); e == nil {
		t.Fatal("tampered AAD accepted")
	}
	b.Seq--
	b.Data = "AAAA"
	if _, e = Open(key, "s2c", b); e == nil {
		t.Fatal("tampered ciphertext accepted")
	}
	b, e = Seal(key, "s2c", 17, 3, p)
	if e != nil {
		t.Fatal(e)
	}
	if os.Getenv("AGENTDECK_WRITE_VECTORS") == "1" {
		v := map[string]any{"root": B64.EncodeToString(root), "salt": B64.EncodeToString(salt), "key": B64.EncodeToString(key), "box": b, "plain": json.RawMessage(p)}
		d, _ := json.MarshalIndent(v, "", "  ")
		if e = os.WriteFile("../../../app/test/fixtures/crypto.json", d, 0644); e != nil {
			t.Fatal(e)
		}
	}
}
func TestPairRoot(t *testing.T) {
	a, _ := ecdh.X25519().GenerateKey(rand.Reader)
	b, _ := ecdh.X25519().GenerateKey(rand.Reader)
	token := Random(32)
	x, e := PairRoot(a, b.PublicKey().Bytes(), token)
	if e != nil {
		t.Fatal(e)
	}
	y, e := PairRoot(b, a.PublicKey().Bytes(), token)
	if e != nil {
		t.Fatal(e)
	}
	if B64.EncodeToString(x) != B64.EncodeToString(y) {
		t.Fatal("pair root mismatch")
	}
	if _, e = PairRoot(a, make([]byte, 32), token); e == nil {
		t.Fatal("low order public key accepted")
	}
}
