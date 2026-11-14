package subscription

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
)

// ApexConfigPathV1 is the original remote-config path, still served and still
// current in content. ApexConfigPathV2 is what the 7.3.x client reads first.
const (
	ApexConfigPathV1 = "/apos/fl_apex.json"
	ApexConfigPathV2 = "/apos/fl_apexv2.json"
)

// DecodeApexConfig turns one config response body into the envelope JSON
// (`{"format":"apex-config-v1","payload":"<base64>","signature":"…"}`).
//
// The v2 file is not the plain envelope its Content-Type claims: it is base64 of
// that same JSON XORed with a 32-byte ASCII key. Obfuscation, not encryption —
// the key is a plain string in the Dart AOT pool — so a wrong key is detected by
// the JSON parse failing, not by a tag.
func DecodeApexConfig(raw []byte, obfuscated bool) ([]byte, error) {
	trimmed := bytes.TrimSpace(bytes.TrimPrefix(raw, []byte("\xef\xbb\xbf")))
	if len(trimmed) == 0 {
		return nil, errors.New("配置为空")
	}
	if !obfuscated {
		return trimmed, nil
	}
	decoded, err := base64.StdEncoding.DecodeString(string(trimmed))
	if err != nil {
		// The client's own decoder tolerates the URL-safe alphabet and missing
		// padding; the panel has shipped both spellings before.
		if decoded, err = b64ToBytes(string(trimmed)); err != nil {
			return nil, fmt.Errorf("配置不是 base64: %w", err)
		}
	}
	key := []byte(apexConfigXORKey)
	out := make([]byte, len(decoded))
	for i := range decoded {
		out[i] = decoded[i] ^ key[i%len(key)]
	}
	return out, nil
}
