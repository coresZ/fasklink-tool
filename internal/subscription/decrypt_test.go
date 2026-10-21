package subscription

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"strings"
	"testing"
)

func encryptGCMTest(plain string) string {
	key, _ := hex.DecodeString(GCMKeyHex)
	block, _ := aes.NewCipher(key)
	gcm, _ := cipher.NewGCM(block)
	iv := make([]byte, gcm.NonceSize())
	_, _ = rand.Read(iv)
	sealed := gcm.Seal(nil, iv, []byte(plain), nil)
	return base64.StdEncoding.EncodeToString(append(iv, sealed...))
}

func obfuscateTest(plain string) string {
	encoded := base64.StdEncoding.EncodeToString([]byte(plain))
	var out strings.Builder
	for _, ch := range encoded {
		index := strings.IndexRune(Base64Alpha, ch)
		if index < 0 {
			out.WriteRune(ch)
		} else {
			out.WriteByte(ObfuscAlpha[index])
		}
	}
	return out.String()
}

func TestDecryptProfileGCM(t *testing.T) {
	yaml := "proxies:\n  - name: test-node\n    type: ss\n    server: example.com\n"
	got, err := DecryptProfile([]byte(encryptGCMTest(yaml)))
	if err != nil {
		t.Fatal(err)
	}
	if got != yaml {
		t.Fatalf("got %q", got)
	}
}

func TestDecryptProfileObfuscated(t *testing.T) {
	yaml := "proxies:\n  - name: test-node\n    server: example.com\n"
	got, err := DecryptProfile([]byte(obfuscateTest(yaml)))
	if err != nil {
		t.Fatal(err)
	}
	if got != yaml {
		t.Fatalf("got %q", got)
	}
}

func TestApplyTemplate(t *testing.T) {
	source := "proxies:\n  - name: test-node\n    type: ss\n    server: example.com\n\nproxy-groups:\n  - name: test\n"
	got, err := ApplyTemplate(source)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "test-node") || !strings.Contains(got, "proxy-groups:") {
		t.Fatalf("template output missing content")
	}
	if strings.Contains(got, "external-controller:") || strings.Contains(got, "secret:") {
		t.Fatalf("template output still contains controller overrides")
	}
}
