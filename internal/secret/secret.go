package secret

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Protector stores account secrets as "dpapi:<base64>" on Windows and falls
// back to AES-256-GCM with .run/.key when DPAPI is unavailable.
type Protector struct {
	runDir string
	mu     sync.Mutex
	key    []byte
}

func New(runDir string) *Protector {
	return &Protector{runDir: runDir}
}

func (p *Protector) Encrypt(plain string) (string, error) {
	if b64, err := protectDPAPI([]byte(plain)); err == nil && b64 != "" {
		return "dpapi:" + b64, nil
	}
	key, err := p.keyBytes()
	if err != nil {
		return "", err
	}
	return "aes:" + encryptAES([]byte(plain), key), nil
}

func (p *Protector) Decrypt(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	switch {
	case strings.HasPrefix(value, "dpapi:"):
		raw, err := unprotectDPAPI(value[len("dpapi:"):])
		if err != nil {
			return "", fmt.Errorf("DPAPI 解密失败: %w", err)
		}
		return string(raw), nil
	case strings.HasPrefix(value, "aes:"):
		key, err := p.keyBytes()
		if err != nil {
			return "", err
		}
		raw, err := decryptAES(value[len("aes:"):], key)
		if err != nil {
			return "", fmt.Errorf("AES 解密失败: %w", err)
		}
		return string(raw), nil
	case strings.HasPrefix(value, "plain:"):
		return value[len("plain:"):], nil
	default:
		// Legacy values without a prefix were stored as plain text.
		return value, nil
	}
}

func (p *Protector) keyBytes() ([]byte, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.key != nil {
		return p.key, nil
	}
	path := filepath.Join(p.runDir, ".key")
	if raw, err := os.ReadFile(path); err == nil && len(raw) == 32 {
		p.key = raw
		return p.key, nil
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, key, 0o600); err != nil {
		return nil, err
	}
	p.key = key
	return p.key, nil
}

// Node's old AES fallback used iv || tag || ciphertext. Keep that layout so
// Go and Node can read each other's .run data during migration.
func encryptAES(plain, key []byte) string {
	block, err := aes.NewCipher(key)
	if err != nil {
		return ""
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return ""
	}
	iv := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(iv); err != nil {
		return ""
	}
	sealed := gcm.Seal(nil, iv, plain, nil)
	tag := sealed[len(sealed)-gcm.Overhead():]
	body := sealed[:len(sealed)-gcm.Overhead()]
	out := make([]byte, 0, len(iv)+len(tag)+len(body))
	out = append(out, iv...)
	out = append(out, tag...)
	out = append(out, body...)
	return base64.StdEncoding.EncodeToString(out)
}

func decryptAES(encoded string, key []byte) ([]byte, error) {
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(raw) < gcm.NonceSize()+gcm.Overhead() {
		return nil, errors.New("short AES payload")
	}
	iv := raw[:gcm.NonceSize()]
	tag := raw[gcm.NonceSize() : gcm.NonceSize()+gcm.Overhead()]
	body := raw[gcm.NonceSize()+gcm.Overhead():]
	combined := append(append([]byte{}, body...), tag...)
	return gcm.Open(nil, iv, combined, nil)
}
