package subscription

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"regexp"
	"strings"
)

var clashYAML = regexp.MustCompile(`(?m)^(proxies|proxy-groups|proxy-providers|rules):`)

func IsClashYAML(text string) bool { return clashYAML.MatchString(text) }

func b64ToBytes(value string) ([]byte, error) {
	normalized := strings.NewReplacer("-", "+", "_", "/").Replace(value)
	normalized = strings.Join(strings.Fields(normalized), "")
	if pad := len(normalized) % 4; pad != 0 {
		normalized += strings.Repeat("=", 4-pad)
	}
	return base64.StdEncoding.DecodeString(normalized)
}

func decryptGCM(value string) (string, error) {
	payload, err := b64ToBytes(value)
	if err != nil {
		return "", err
	}
	if len(payload) < 28 {
		return "", errors.New("short AES-GCM payload")
	}
	key, err := hex.DecodeString(GCMKeyHex)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	plain, err := gcm.Open(nil, payload[:12], payload[12:], nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

func undoObfuscatedBase64(text string) string {
	var out strings.Builder
	out.Grow(len(text))
	for _, ch := range text {
		index := strings.IndexRune(ObfuscAlpha, ch)
		if index < 0 {
			out.WriteRune(ch)
		} else {
			out.WriteByte(Base64Alpha[index])
		}
	}
	return out.String()
}

// DecryptProfile mirrors transferSub.local.mjs: raw YAML, XOR-restored YAML,
// AES-GCM, or obfuscated-base64 YAML.
func DecryptProfile(raw []byte) (string, error) {
	diskKey := []byte(DiskXORKey)
	restored := make([]byte, len(raw))
	for i, b := range raw {
		restored[i] = b ^ diskKey[i%len(diskKey)]
	}
	candidates := []string{strings.TrimSpace(string(raw)), strings.TrimSpace(string(restored))}
	for _, candidate := range candidates {
		if IsClashYAML(candidate) {
			return candidate, nil
		}
		if yaml, err := decryptGCM(candidate); err == nil && IsClashYAML(yaml) {
			return yaml, nil
		}
		if decoded, err := b64ToBytes(undoObfuscatedBase64(candidate)); err == nil && IsClashYAML(string(decoded)) {
			return string(decoded), nil
		}
	}
	return "", errors.New("无法识别或解密订阅，请检查链接，或客户端密钥是否再次更新")
}
