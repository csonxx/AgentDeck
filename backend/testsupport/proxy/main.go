// Local integration fixture: a TLS-terminating reverse proxy with a generated
// test CA. No user certificates, credentials, or real session logs are used.
package main

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
	"flag"
	"log"
	"math/big"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"time"
)

func main() {
	dir := flag.String("dir", "", "temporary fixture directory")
	listen := flag.String("listen", "", "loopback TLS address")
	upstream := flag.String("upstream", "", "loopback HTTP backend")
	flag.Parse()
	if err := run(*dir, *listen, *upstream); err != nil {
		log.Fatal(err)
	}
}

func run(dir, listen, upstream string) error {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	now := time.Now()
	ca := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "AgentDeck test CA"}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	der, err := x509.CreateCertificate(rand.Reader, ca, ca, &key.PublicKey, key)
	if err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(dir, "ca.pem"), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600); err != nil {
		return err
	}
	var certs []tls.Certificate
	for i := int64(2); i <= 3; i++ {
		leafKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			return err
		}
		leaf := &x509.Certificate{SerialNumber: big.NewInt(i), Subject: pkix.Name{CommonName: "localhost"}, DNSNames: []string{"localhost"}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
		leafDER, err := x509.CreateCertificate(rand.Reader, leaf, ca, &leafKey.PublicKey, key)
		if err != nil {
			return err
		}
		certs = append(certs, tls.Certificate{Certificate: [][]byte{leafDER, der}, PrivateKey: leafKey})
		parsed, err := x509.ParseCertificate(leafDER)
		if err != nil {
			return err
		}
		h := sha256.Sum256(parsed.RawSubjectPublicKeyInfo)
		name := "pin-before"
		if i == 3 {
			name = "pin-after"
		}
		if err = os.WriteFile(filepath.Join(dir, name), []byte(hex.EncodeToString(h[:])), 0600); err != nil {
			return err
		}
	}
	u, err := url.Parse(upstream)
	if err != nil {
		return err
	}
	proxy := httputil.NewSingleHostReverseProxy(u)
	proxy.ErrorLog = log.New(os.Stderr, "test proxy error: ", 0)
	srv := &http.Server{Addr: listen, Handler: proxy, ReadHeaderTimeout: 5 * time.Second, TLSConfig: &tls.Config{MinVersion: tls.VersionTLS13, GetCertificate: func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
		if _, err := os.Stat(filepath.Join(dir, "rotate")); err == nil {
			return &certs[1], nil
		}
		return &certs[0], nil
	}}}
	return srv.ListenAndServeTLS("", "")
}
