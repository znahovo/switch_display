package deskflow

import (
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGenerateCertAndFingerprint(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tls", "deskflow.pem")

	fp, err := GenerateCert(path)
	if err != nil {
		t.Fatalf("GenerateCert() error = %v", err)
	}
	if !strings.HasPrefix(fp, "v2:sha256:") {
		t.Fatalf("fingerprint = %q, want v2:sha256: prefix", fp)
	}
	if len(fp) != len("v2:sha256:")+64 {
		t.Fatalf("fingerprint len = %d, want %d", len(fp), len("v2:sha256:")+64)
	}
	if strings.ToLower(fp) != fp {
		t.Fatalf("fingerprint must be lowercase: %q", fp)
	}

	// 文件必须同时含证书与私钥（Deskflow 用同一个文件读两者）。
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	var types []string
	for rest := data; ; {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		types = append(types, block.Type)
	}
	if len(types) != 2 || types[0] != "CERTIFICATE" || types[1] != "RSA PRIVATE KEY" {
		t.Fatalf("pem blocks = %v, want [CERTIFICATE RSA PRIVATE KEY]", types)
	}

	// 证书属性必须与 Deskflow 生成的一致。
	block, _ := pem.Decode(data)
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("ParseCertificate() error = %v", err)
	}
	if cert.Subject.CommonName != certCommonName {
		t.Fatalf("CN = %q, want %q", cert.Subject.CommonName, certCommonName)
	}
	if got := cert.NotAfter.Sub(cert.NotBefore); got < 300*24*time.Hour {
		t.Fatalf("validity = %v, want ~365 days", got)
	}
}

func TestEnsureCertIdempotent(t *testing.T) {
	dir := t.TempDir()

	fp1, regen1, err := EnsureCert(dir)
	if err != nil {
		t.Fatalf("first EnsureCert() error = %v", err)
	}
	if !regen1 {
		t.Fatal("first EnsureCert() regenerated = false, want true")
	}

	fp2, regen2, err := EnsureCert(dir)
	if err != nil {
		t.Fatalf("second EnsureCert() error = %v", err)
	}
	if regen2 {
		t.Fatal("second EnsureCert() regenerated = true, want false")
	}
	if fp1 != fp2 {
		t.Fatalf("fingerprint changed: %q -> %q", fp1, fp2)
	}
}

func TestEnsureCertRegeneratesOnGarbage(t *testing.T) {
	dir := t.TempDir()
	paths := TLSPaths(dir)
	if err := os.MkdirAll(filepath.Dir(paths.Cert), 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(paths.Cert, []byte("not a pem"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	fp, regen, err := EnsureCert(dir)
	if err != nil {
		t.Fatalf("EnsureCert() error = %v", err)
	}
	if !regen {
		t.Fatal("EnsureCert() regenerated = false, want true for garbage file")
	}
	if got, err := readFingerprintFromPEM(paths.Cert); err != nil {
		t.Fatalf("readFingerprintFromPEM() error = %v", err)
	} else if got != fp {
		t.Fatalf("fingerprint mismatch: %q vs %q", got, fp)
	}
}

func TestEnsureCertRegeneratesOnMissingKey(t *testing.T) {
	dir := t.TempDir()
	paths := TLSPaths(dir)
	if err := os.MkdirAll(filepath.Dir(paths.Cert), 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	// 只有证书、没有私钥：core 会在 loadCertificate 阶段失败，必须重建。
	if _, err := GenerateCert(paths.Cert); err != nil {
		t.Fatalf("GenerateCert() error = %v", err)
	}
	data, err := os.ReadFile(paths.Cert)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	certBlock, _ := pem.Decode(data)
	if err := os.WriteFile(paths.Cert, pem.EncodeToMemory(certBlock), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	if _, regen, err := EnsureCert(dir); err != nil {
		t.Fatalf("EnsureCert() error = %v", err)
	} else if !regen {
		t.Fatal("EnsureCert() regenerated = false, want true when private key is missing")
	}
}

func TestTLSPaths(t *testing.T) {
	p := TLSPaths("/base")
	if p.Cert != "/base/tls/deskflow.pem" {
		t.Fatalf("Cert = %q", p.Cert)
	}
	if p.TrustedClients != "/base/tls/trusted-clients" {
		t.Fatalf("TrustedClients = %q", p.TrustedClients)
	}
	if p.TrustedServers != "/base/tls/trusted-servers" {
		t.Fatalf("TrustedServers = %q", p.TrustedServers)
	}
}
