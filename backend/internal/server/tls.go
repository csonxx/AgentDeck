package server

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"
)

func Certificate(dir, certPath, keyPath string) (tls.Certificate, string, error) {
	if certPath == "" {
		certPath = filepath.Join(dir, "tls.pem")
		keyPath = filepath.Join(dir, "tls.key")
		if _, e := os.Stat(certPath); os.IsNotExist(e) {
			key, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
			if e != nil {
				return tls.Certificate{}, "", e
			}
			serial, e := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
			if e != nil {
				return tls.Certificate{}, "", e
			}
			tmpl := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: "AgentDeck"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().AddDate(1, 0, 0), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, DNSNames: []string{"localhost"}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")}}
			der, e := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
			if e != nil {
				return tls.Certificate{}, "", e
			}
			priv, e := x509.MarshalECPrivateKey(key)
			if e != nil {
				return tls.Certificate{}, "", e
			}
			if e = os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600); e != nil {
				return tls.Certificate{}, "", e
			}
			if e = os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: priv}), 0600); e != nil {
				return tls.Certificate{}, "", e
			}
		}
	}
	cert, e := tls.LoadX509KeyPair(certPath, keyPath)
	if e != nil {
		return cert, "", e
	}
	leaf, e := x509.ParseCertificate(cert.Certificate[0])
	if e != nil {
		return cert, "", e
	}
	hash := sha256.Sum256(leaf.RawSubjectPublicKeyInfo)
	return cert, hex.EncodeToString(hash[:]), nil
}
