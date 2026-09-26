package app

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
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Each installation has its own CA. Only root.crt is shared with clients.
func LocalCAFile(c Config) string { return filepath.Join(c.DataDir, "certs", "root.crt") }
func localCAKey(c Config) string  { return filepath.Join(c.DataDir, "certs", "root.key") }

func readCertificate(path string) (*x509.Certificate, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(data)
	if block == nil || block.Type != "CERTIFICATE" {
		return nil, errors.New("invalid certificate file")
	}
	return x509.ParseCertificate(block.Bytes)
}

// TrustCertificate also supports installations created before local CAs existed.
func TrustCertificate(c Config) ([]byte, *x509.Certificate, error) {
	leaf, err := readCertificate(c.CertFile)
	if err != nil {
		return nil, nil, err
	}
	cert, err := readCertificate(LocalCAFile(c))
	if os.IsNotExist(err) {
		cert = leaf
	} else if err != nil {
		return nil, nil, err
	} else if leaf.CheckSignatureFrom(cert) != nil {
		return nil, nil, errors.New("the server certificate does not match its local certificate authority; check the configured TLS files")
	}
	return cert.Raw, cert, nil
}

func CertificateFingerprint(cert *x509.Certificate) string {
	hash := sha256.Sum256(cert.Raw)
	return hex.EncodeToString(hash[:])
}

func certificateTemplate(name string, days int) (*x509.Certificate, error) {
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, err
	}
	return &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: name}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Duration(days) * 24 * time.Hour), BasicConstraintsValid: true}, nil
}

func writeExclusive(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	closeErr := f.Close()
	if err != nil || closeErr != nil {
		_ = os.Remove(path)
		return errors.Join(err, closeErr)
	}
	return nil
}

func writePair(certPath, keyPath string, der []byte, key *ecdsa.PrivateKey) error {
	pk, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return err
	}
	if err = writeExclusive(keyPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pk})); err != nil {
		return err
	}
	if err = writeExclusive(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})); err != nil {
		_ = os.Remove(keyPath)
	}
	return err
}

func GenerateCert(c Config) error {
	for _, path := range []string{LocalCAFile(c), localCAKey(c), c.CertFile, c.KeyFile} {
		if _, err := Contained(c.DataDir, path); err != nil {
			return err
		}
	}
	for _, path := range []string{c.CertFile, c.KeyFile} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			return fmt.Errorf("certificate generation will not overwrite %s", path)
		}
	}
	ca, err := readCertificate(LocalCAFile(c))
	if os.IsNotExist(err) {
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			return err
		}
		root, err := certificateTemplate("BrrpBrrp Local CA", 3650)
		if err != nil {
			return err
		}
		root.IsCA, root.MaxPathLenZero = true, true
		root.KeyUsage = x509.KeyUsageCertSign | x509.KeyUsageCRLSign
		der, err := x509.CreateCertificate(rand.Reader, root, root, &key.PublicKey, key)
		if err != nil {
			return err
		}
		if err = writePair(LocalCAFile(c), localCAKey(c), der, key); err != nil {
			return err
		}
		ca, err = x509.ParseCertificate(der)
		if err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	if !ca.IsCA || time.Until(ca.NotAfter) < 24*time.Hour {
		return errors.New("the local certificate authority has expired or is invalid; replace it and trust the new root on each device")
	}
	pair, err := tls.LoadX509KeyPair(LocalCAFile(c), localCAKey(c))
	if err != nil {
		return err
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	leaf, err := certificateTemplate("BrrpBrrp", 90)
	if err != nil {
		return err
	}
	if leaf.NotAfter.After(ca.NotAfter) {
		leaf.NotAfter = ca.NotAfter
	}
	leaf.KeyUsage = x509.KeyUsageDigitalSignature
	leaf.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}
	leaf.DNSNames = []string{"localhost"}
	leaf.IPAddresses = []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")}
	u, _ := url.Parse(c.BaseURL)
	for _, host := range []string{c.Domain, u.Hostname(), c.VoiceExternalIP} {
		if host == "" {
			continue
		}
		if ip := net.ParseIP(host); ip != nil {
			leaf.IPAddresses = append(leaf.IPAddresses, ip)
		} else {
			leaf.DNSNames = append(leaf.DNSNames, host)
		}
	}
	der, err := x509.CreateCertificate(rand.Reader, leaf, ca, &key.PublicKey, pair.PrivateKey)
	if err != nil {
		return err
	}
	return writePair(c.CertFile, c.KeyFile, der, key)
}

// RepairCertificate runs under the exclusive data lock, with the service stopped.
// Keep the old pair for rollback, and never replace an operator-supplied certificate.
func RepairCertificate(c Config) error {
	if c.TLSMode != "auto" {
		return errors.New("certificate repair is available only for local auto TLS; manage supplied/proxy certificates with their issuer")
	}
	cert, err := readCertificate(c.CertFile)
	if err != nil {
		return err
	}
	ca, caErr := readCertificate(LocalCAFile(c))
	legacy := legacyCertificate(cert)
	managed := caErr == nil && cert.CheckSignatureFrom(ca) == nil
	if !legacy && !managed {
		return errors.New("this certificate was not generated by BrrpBrrp; it has been left unchanged")
	}
	backup, err := os.MkdirTemp(filepath.Join(c.DataDir, "certs"), "previous-")
	if err != nil {
		return err
	}
	oldCert, oldKey := filepath.Join(backup, "tls.crt"), filepath.Join(backup, "tls.key")
	if err := os.Rename(c.CertFile, oldCert); err != nil {
		return err
	}
	if err := os.Rename(c.KeyFile, oldKey); err != nil {
		_ = os.Rename(oldCert, c.CertFile)
		return err
	}
	if err := GenerateCert(c); err != nil {
		_ = os.Remove(c.CertFile)
		_ = os.Remove(c.KeyFile)
		certErr, keyErr := os.Rename(oldCert, c.CertFile), os.Rename(oldKey, c.KeyFile)
		return errors.Join(err, certErr, keyErr)
	}
	return nil
}

// Renew only our managed leaf, at startup. Existing self-signed and custom
// certificates keep their current trust until the operator chooses repair.
func RenewLocalCertificate(c Config) error {
	ca, err := readCertificate(LocalCAFile(c))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	cert, err := readCertificate(c.CertFile)
	if err != nil {
		return err
	}
	if cert.CheckSignatureFrom(ca) != nil {
		return nil
	}
	u, _ := url.Parse(c.BaseURL)
	if time.Until(cert.NotAfter) > 30*24*time.Hour && cert.VerifyHostname(u.Hostname()) == nil {
		return nil
	}
	return RepairCertificate(c)
}

// Refresh on a TLS handshake at most twice a day, including on servers that
// remain up for months. Handshakes see a complete certificate/key pair.
func localCertificateSource(c Config) (func(*tls.ClientHelloInfo) (*tls.Certificate, error), error) {
	pair, err := tls.LoadX509KeyPair(c.CertFile, c.KeyFile)
	if err != nil {
		return nil, err
	}
	current := &pair
	var mu sync.Mutex
	next := time.Now().Add(12 * time.Hour)
	return func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
		mu.Lock()
		defer mu.Unlock()
		if !time.Now().Before(next) {
			if err := RenewLocalCertificate(c); err != nil {
				return nil, err
			}
			loaded, err := tls.LoadX509KeyPair(c.CertFile, c.KeyFile)
			if err != nil {
				return nil, err
			}
			current = &loaded
			next = time.Now().Add(12 * time.Hour)
		}
		return current, nil
	}, nil
}

// Match the original generator's extensions and loopback SANs, not just its name.
func legacyCertificate(cert *x509.Certificate) bool {
	if cert.Subject.CommonName != "BrrpBrrp" || cert.IsCA || !cert.BasicConstraintsValid || cert.KeyUsage != x509.KeyUsageDigitalSignature || len(cert.ExtKeyUsage) != 1 || cert.ExtKeyUsage[0] != x509.ExtKeyUsageServerAuth {
		return false
	}
	localhost, ipv4, ipv6 := false, false, false
	for _, name := range cert.DNSNames {
		localhost = localhost || name == "localhost"
	}
	for _, ip := range cert.IPAddresses {
		ipv4 = ipv4 || ip.Equal(net.ParseIP("127.0.0.1"))
		ipv6 = ipv6 || ip.Equal(net.ParseIP("::1"))
	}
	return localhost && ipv4 && ipv6 && cert.CheckSignature(cert.SignatureAlgorithm, cert.RawTBSCertificate, cert.Signature) == nil
}
