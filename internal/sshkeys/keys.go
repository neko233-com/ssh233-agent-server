package sshkeys

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"

	"golang.org/x/crypto/ssh"
)

type KeyPair struct {
	PrivateKey  string
	PublicKey   string
	Fingerprint string
	Signer      ssh.Signer
}

func Generate(label string) (*KeyPair, error) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 4096)
	if err != nil {
		return nil, fmt.Errorf("generate rsa key: %w", err)
	}

	signer, err := ssh.NewSignerFromKey(privateKey)
	if err != nil {
		return nil, fmt.Errorf("create signer: %w", err)
	}

	pubKey := ssh.MarshalAuthorizedKey(signer.PublicKey())
	privPEM := encodePrivateKey(privateKey)

	fp := Fingerprint(signer.PublicKey())

	return &KeyPair{
		PrivateKey:  privPEM,
		PublicKey:   string(pubKey),
		Fingerprint: fp,
		Signer:      signer,
	}, nil
}

func ParsePrivateKey(pem string) (*KeyPair, error) {
	signer, err := ssh.ParsePrivateKey([]byte(pem))
	if err != nil {
		return nil, fmt.Errorf("parse private key: %w", err)
	}
	pubKey := ssh.MarshalAuthorizedKey(signer.PublicKey())
	return &KeyPair{
		PrivateKey:  pem,
		PublicKey:   string(pubKey),
		Fingerprint: Fingerprint(signer.PublicKey()),
		Signer:      signer,
	}, nil
}

func Fingerprint(pub ssh.PublicKey) string {
	hash := sha256.Sum256(pub.Marshal())
	return "SHA256:" + base64.StdEncoding.EncodeToString(hash[:])
}

func encodePrivateKey(key *rsa.PrivateKey) string {
	block := &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	}
	return string(pem.EncodeToMemory(block))
}

// UploadPublicKey installs the public key into ~/.ssh/authorized_keys on the target via an active session.
func UploadPublicKey(session *ssh.Session, publicKey string) error {
	script := fmt.Sprintf(`
mkdir -p ~/.ssh && chmod 700 ~/.ssh
touch ~/.ssh/authorized_keys && chmod 600 ~/.ssh/authorized_keys
grep -qF '%s' ~/.ssh/authorized_keys 2>/dev/null || echo '%s' >> ~/.ssh/authorized_keys
`, trimNewline(publicKey), trimNewline(publicKey))

	return session.Run(script)
}

func trimNewline(s string) string {
	if len(s) > 0 && s[len(s)-1] == '\n' {
		return s[:len(s)-1]
	}
	return s
}
