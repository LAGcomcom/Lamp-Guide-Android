package remote

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"strings"
	"time"
)

// The QoderCN client authenticates its model calls with a **cosy** credential,
// not with the OAuth device token. The device token only proves the login; every
// model request is signed with a cosy_key + cosy headers (see headers()), and
// the cosy_key is minted locally by the client from a short random AES key that
// is RSA-wrapped, plus an AES-CBC blob holding the user identity.
//
// Signing in from outside the IDE therefore needs both steps, and this file is
// step two: mint (cosy_key, encrypt_user_info) from the values the device login
// returned. It mirrors the client's own implementation byte for byte — key
// length, IV == first block of the key, the exact JSON field names, and the
// padding. Sizes were verified against a real IDE cache: 16 raw bytes become a
// 172-byte cosy_key and a 108-byte user blob.
//
// ⚠️ The public key below is the client's own embedded constant; it encrypts,
// it cannot decrypt anything. It lives here because a login that stopped at the
// device token would produce a credential that lists models as 401.
const qoderCosyPublicKey = `-----BEGIN PUBLIC KEY-----
MIGfMA0GCSqGSIb3DQEBAQUAA4GNADCBiQKBgQDA8iMH5c02LilrsERw9t6Pv5Nc
4k6Pz1EaDicBMpdpxKduSZu5OANqUq8er4GM95omAGIOPOh+Nx0spthYA2BqGz+l
6HRkPJ7S236FZz73In/KVuLnwI8JJ2CbuJap8kvheCCZpmAWpb/cPx/3Vr/J6I17
XcW+ML9FoCI6AOvOzwIDAQAB
-----END PUBLIC KEY-----`

// cosyIdentity is the plaintext the client encrypts into encrypt_user_info.
// Field names are the consumer's, not ours: aid stays empty because the desktop
// client sends it empty for a personal account.
type cosyIdentity struct {
	UID     string `json:"uid"`
	AID     string `json:"aid"`
	Name    string `json:"name"`
	Email   string `json:"email"`
	IDToken string `json:"security_oauth_token"`
}

// MintCosyCredential derives the cosy pair a model call needs from an
// authenticated identity. key and info are the two values store as
// cosy_key / encrypt_user_info.
func MintCosyCredential(uid, name, email, accessToken string) (key string, info string, err error) {
	if strings.TrimSpace(uid) == "" || strings.TrimSpace(accessToken) == "" {
		return "", "", fmt.Errorf("mint cosy credential: uid and access token are both required")
	}

	// The client takes the first 16 characters of a dashless UUID as the AES
	// key — 16 bytes exactly, so no KDF in between.
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", "", err
	}
	aesKey := []byte(strings.ReplaceAll(uuidString(), "-", "")[:16])

	block, err := aes.NewCipher(aesKey)
	if err != nil {
		return "", "", err
	}
	plain, err := json.Marshal(cosyIdentity{
		UID: uid, AID: "", Name: name, Email: email, IDToken: accessToken,
	})
	if err != nil {
		return "", "", err
	}
	// CBC with the key's first block as IV is the client's choice, not ours.
	// It is also the reason the raw key must be exactly one AES block.
	padded := pkcs7Pad(plain, block.BlockSize())
	sealed := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, aesKey).CryptBlocks(sealed, padded)
	info = base64.StdEncoding.EncodeToString(sealed)

	pub, err := cosyPublicKey()
	if err != nil {
		return "", "", err
	}
	wrapped, err := rsa.EncryptPKCS1v15(rand.Reader, pub, aesKey)
	if err != nil {
		return "", "", err
	}
	key = base64.StdEncoding.EncodeToString(wrapped)
	return key, info, nil
}

func cosyPublicKey() (*rsa.PublicKey, error) {
	block, _ := pem.Decode([]byte(qoderCosyPublicKey))
	if block == nil {
		return nil, fmt.Errorf("cosy public key is not a PEM block")
	}
	parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse cosy public key: %w", err)
	}
	pub, ok := parsed.(*rsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("cosy public key is not RSA")
	}
	return pub, nil
}

func pkcs7Pad(data []byte, size int) []byte {
	if size <= 0 {
		return data
	}
	pad := size - len(data)%size
	out := make([]byte, len(data)+pad)
	copy(out, data)
	for i := len(data); i < len(out); i++ {
		out[i] = byte(pad)
	}
	return out
}

// uuidString is a dashless-safe v4 UUID (the 16-char slice above needs it to be
// random in the first block, which is what the version nibble depends on).
func uuidString() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	h := fmt.Sprintf("%x", b)
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
}

// cosyRequestID mirrors the client's requestId: a dashless UUID, used only for
// correlation on the upstream side.
func cosyRequestID() string {
	sum := md5.Sum([]byte(fmt.Sprintf("%d", time.Now().UnixNano())))
	return fmt.Sprintf("%x", sum)
}
