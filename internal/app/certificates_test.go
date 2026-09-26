package app

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func certConfig(t *testing.T) Config {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "certs"), 0700); err != nil {
		t.Fatal(err)
	}
	return Config{DataDir: root, CertFile: filepath.Join(root, "certs", "tls.crt"), KeyFile: filepath.Join(root, "certs", "tls.key"), TLSMode: "auto", BaseURL: "https://192.168.1.20:8082"}
}
func verifyLocal(t *testing.T, c Config) *x509.Certificate {
	t.Helper()
	root, err := readCertificate(LocalCAFile(c))
	if err != nil {
		t.Fatal(err)
	}
	if !root.IsCA || root.KeyUsage&x509.KeyUsageCertSign == 0 || !root.MaxPathLenZero {
		t.Fatal("root is not a constrained certificate authority")
	}
	leaf, err := readCertificate(c.CertFile)
	if err != nil {
		t.Fatal(err)
	}
	if leaf.IsCA {
		t.Fatal("server certificate is a CA")
	}
	pool := x509.NewCertPool()
	pool.AddCert(root)
	if _, err := leaf.Verify(x509.VerifyOptions{Roots: pool, DNSName: "192.168.1.20"}); err != nil {
		t.Fatal(err)
	}
	if _, err := leaf.Verify(x509.VerifyOptions{Roots: pool, DNSName: "192.168.1.21"}); err == nil {
		t.Fatal("accepted a different host")
	}
	if _, err := tls.LoadX509KeyPair(c.CertFile, c.KeyFile); err != nil {
		t.Fatal(err)
	}
	return leaf
}
func TestLocalCertificatesHaveTrustableCAAndPreserveRootOnRepair(t *testing.T) {
	c := certConfig(t)
	if err := GenerateCert(c); err != nil {
		t.Fatal(err)
	}
	leaf := verifyLocal(t, c)
	if time.Until(leaf.NotAfter) > 91*24*time.Hour {
		t.Fatal("leaf validity too long")
	}
	root, _ := os.ReadFile(LocalCAFile(c))
	key, _ := os.ReadFile(c.KeyFile)
	old, _ := os.ReadFile(c.CertFile)
	if err := GenerateCert(c); err == nil {
		t.Fatal("overwrote existing certificate")
	}
	if err := RepairCertificate(c); err != nil {
		t.Fatal(err)
	}
	verifyLocal(t, c)
	nextRoot, _ := os.ReadFile(LocalCAFile(c))
	if !bytes.Equal(root, nextRoot) {
		t.Fatal("repair changed the trusted root")
	}
	previous, _ := filepath.Glob(filepath.Join(c.DataDir, "certs", "previous-*"))
	if len(previous) != 1 {
		t.Fatal(previous)
	}
	savedCert, _ := os.ReadFile(filepath.Join(previous[0], "tls.crt"))
	savedKey, _ := os.ReadFile(filepath.Join(previous[0], "tls.key"))
	if !bytes.Equal(old, savedCert) || !bytes.Equal(key, savedKey) {
		t.Fatal("rollback pair not preserved")
	}
	der, cert, err := TrustCertificate(c)
	if err != nil || !bytes.Equal(der, cert.Raw) || !cert.IsCA {
		t.Fatal("download does not contain the public root")
	}
}
func TestManagedRenewalUpdatesExpiringLeafAndHostname(t *testing.T) {
	c := certConfig(t)
	if err := GenerateCert(c); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(c.CertFile)
	if err := RenewLocalCertificate(c); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(c.CertFile)
	if !bytes.Equal(before, after) {
		t.Fatal("unnecessary renewal")
	}
	leaf, _ := readCertificate(c.CertFile)
	ca, _ := readCertificate(LocalCAFile(c))
	rootPair, _ := tls.LoadX509KeyPair(LocalCAFile(c), localCAKey(c))
	pair, _ := tls.LoadX509KeyPair(c.CertFile, c.KeyFile)
	leaf.NotAfter = time.Now().Add(5 * 24 * time.Hour)
	der, err := x509.CreateCertificate(rand.Reader, leaf, ca, pair.PrivateKey.(*ecdsa.PrivateKey).Public(), rootPair.PrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(c.CertFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600)
	if err := RenewLocalCertificate(c); err != nil {
		t.Fatal(err)
	}
	next := verifyLocal(t, c)
	if time.Until(next.NotAfter) < 80*24*time.Hour {
		t.Fatal("expiring leaf not renewed")
	}
	c.BaseURL = "https://192.168.1.22:8082"
	if err := RenewLocalCertificate(c); err != nil {
		t.Fatal(err)
	}
	next, _ = readCertificate(c.CertFile)
	if err := next.VerifyHostname("192.168.1.22"); err != nil {
		t.Fatal(err)
	}
}
func TestLegacyRepairIsExplicitAndCustomCertificatesArePreserved(t *testing.T) {
	for _, name := range []string{"BrrpBrrp", "Operator Certificate"} {
		t.Run(name, func(t *testing.T) {
			c := certConfig(t)
			key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
			leaf, _ := certificateTemplate(name, 365)
			leaf.DNSNames = []string{"localhost"}
			leaf.IPAddresses = []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")}
			leaf.KeyUsage = x509.KeyUsageDigitalSignature
			leaf.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}
			der, err := x509.CreateCertificate(rand.Reader, leaf, leaf, &key.PublicKey, key)
			if err != nil {
				t.Fatal(err)
			}
			if err := writePair(c.CertFile, c.KeyFile, der, key); err != nil {
				t.Fatal(err)
			}
			before, _ := os.ReadFile(c.CertFile)
			if err := RenewLocalCertificate(c); err != nil {
				t.Fatal(err)
			}
			after, _ := os.ReadFile(c.CertFile)
			if !bytes.Equal(before, after) {
				t.Fatal("silently migrated existing trust")
			}
			err = RepairCertificate(c)
			if name == "BrrpBrrp" {
				if err != nil {
					t.Fatal(err)
				}
				verifyLocal(t, c)
			} else {
				if err == nil {
					t.Fatal("replaced supplied certificate")
				}
				after, _ = os.ReadFile(c.CertFile)
				if !bytes.Equal(before, after) {
					t.Fatal("changed supplied bytes")
				}
			}
		})
	}
}
func TestFailedRepairRestoresPreviousPair(t *testing.T) {
	c := certConfig(t)
	if err := GenerateCert(c); err != nil {
		t.Fatal(err)
	}
	cert, _ := os.ReadFile(c.CertFile)
	key, _ := os.ReadFile(c.KeyFile)
	if err := os.Remove(localCAKey(c)); err != nil {
		t.Fatal(err)
	}
	if err := RepairCertificate(c); err == nil {
		t.Fatal("repair succeeded without CA key")
	}
	nextCert, _ := os.ReadFile(c.CertFile)
	nextKey, _ := os.ReadFile(c.KeyFile)
	if !bytes.Equal(cert, nextCert) || !bytes.Equal(key, nextKey) {
		t.Fatal("failed repair lost previous pair")
	}
}
