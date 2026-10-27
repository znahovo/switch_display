// Package deskflow 把 Deskflow core 当作外部进程来驱动：
// 定位可执行文件、生成 TLS 证书、写配置与信任库、拉起/回收进程。
//
// 它实现 application/linkapp 里定义的端口，不 import 任何用例包（R3）。
package deskflow

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"time"
)

// 证书参数与 Deskflow GUI 的 TlsUtility 保持一致，
// 这样我们用生成的文件替换 GUI 生成的文件时行为完全等价。
const (
	// certCommonName 是证书的 subject/issuer CN（Deskflow 硬编码 "Deskflow"）。
	certCommonName = "Deskflow"
	// certValidity 是有效期（Deskflow 用 365 天）。
	certValidity = 365 * 24 * time.Hour
	// certKeyBits 是 RSA 位数（Deskflow 默认 2048）。
	certKeyBits = 2048
	// certFileName 是证书文件名（Deskflow 默认值是 <settings>/tls/deskflow.pem）。
	certFileName = "deskflow.pem"
)

// CertPaths 是 TLS 相关文件的路径集合。
type CertPaths struct {
	// Cert 是证书 + 私钥的 PEM 文件（同一个文件，Deskflow 的要求）。
	Cert string
	// TrustedClients 是 server 端信任的客机指纹库。
	TrustedClients string
	// TrustedServers 是 client 端信任的主机指纹库。
	TrustedServers string
}

// TLSPaths 返回 dir 下的 TLS 文件路径。
//
// 布局与 Deskflow 的 Settings::tlsDir() 一致（<settingsPath>/tls），
// 只是 settingsPath 换成了我们的托管目录。
func TLSPaths(dir string) CertPaths {
	tlsDir := filepath.Join(dir, "tls")
	return CertPaths{
		Cert:           filepath.Join(tlsDir, certFileName),
		TrustedClients: filepath.Join(tlsDir, "trusted-clients"),
		TrustedServers: filepath.Join(tlsDir, "trusted-servers"),
	}
}

// EnsureCert 确保证书存在且在有效期内，返回其 SHA-256 指纹
// （`v2:sha256:<64 位小写 hex>`，即 Deskflow 信任库的落盘格式）。
//
// 若文件缺失、解析失败、或已过期/即将过期，就重新生成一份。
// 重新生成会让旧指纹失效，调用方必须清空已配对列表。
func EnsureCert(dir string) (fingerprint string, regenerated bool, err error) {
	paths := TLSPaths(dir)
	if fp, ok := readCertFingerprint(paths.Cert); ok {
		return fp, false, nil
	}
	fp, err := GenerateCert(paths.Cert)
	if err != nil {
		return "", false, err
	}
	return fp, true, nil
}

// GenerateCert 在 path 生成自签证书（RSA 2048，CN=Deskflow，365 天），
// 证书与私钥写入同一个 PEM 文件，并返回 SHA-256 指纹。
func GenerateCert(path string) (string, error) {
	key, err := rsa.GenerateKey(rand.Reader, certKeyBits)
	if err != nil {
		return "", fmt.Errorf("generate rsa key: %w", err)
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return "", fmt.Errorf("generate serial: %w", err)
	}

	now := time.Now()
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: certCommonName},
		Issuer:                pkix.Name{CommonName: certCommonName},
		NotBefore:             now.Add(-time.Minute), // 容忍对端时钟略慢
		NotAfter:              now.Add(certValidity),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
	}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return "", fmt.Errorf("create certificate: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", fmt.Errorf("create tls dir: %w", err)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	// Deskflow 用 SSL_CTX_use_certificate_file + SSL_CTX_use_PrivateKey_file
	// 读同一个文件，所以这里必须把两段拼在一起。
	blob := append(append([]byte{}, certPEM...), keyPEM...)
	if err := writeFileAtomic(path, blob, 0o600); err != nil {
		return "", err
	}

	return Fingerprint(der), nil
}

// readCertFingerprint 读取已有证书并返回指纹；不存在、解析失败、过期或
// 私钥缺失都返回 ok=false（让调用方重新生成）。
func readCertFingerprint(path string) (string, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}

	// 逐个 PEM 段扫描：证书可能在前面或后面，私钥必须存在。
	var cert *x509.Certificate
	hasKey := false
	for {
		var block *pem.Block
		block, data = pem.Decode(data)
		if block == nil {
			break
		}
		switch block.Type {
		case "CERTIFICATE":
			if cert != nil {
				continue
			}
			c, err := x509.ParseCertificate(block.Bytes)
			if err != nil {
				return "", false
			}
			cert = c
		case "RSA PRIVATE KEY", "PRIVATE KEY", "EC PRIVATE KEY":
			hasKey = true
		}
	}
	if cert == nil || !hasKey {
		return "", false
	}
	// 留 7 天余量：临期证书在长时间运行后会突然失效，不如提前换。
	if time.Now().Add(7 * 24 * time.Hour).After(cert.NotAfter) {
		return "", false
	}
	return Fingerprint(cert.Raw), true
}

// Fingerprint 把 DER 证书算成 Deskflow 信任库行的格式。
//
// 对应 `Fingerprint::toDbLine()`：`v2:sha256:<hex 小写>`。
// 注意 GUI 展示用的是大写带冒号形式，写文件必须用小写无冒号。
func Fingerprint(der []byte) string {
	sum := sha256.Sum256(der)
	return "v2:sha256:" + hex.EncodeToString(sum[:])
}

// readFingerprintFromPEM 从 PEM 文件算指纹（供测试与自检使用）。
func readFingerprintFromPEM(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	block, _ := pem.Decode(data)
	if block == nil || block.Type != "CERTIFICATE" {
		return "", fmt.Errorf("no certificate in %s", path)
	}
	return Fingerprint(block.Bytes), nil
}

// writeFileAtomic 与 persistence 的写法同源：临时文件 → fsync → rename。
func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create dir %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("sync temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Chmod(tmpName, mode); err != nil {
		return fmt.Errorf("chmod temp file: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("replace %s: %w", path, err)
	}
	return nil
}
