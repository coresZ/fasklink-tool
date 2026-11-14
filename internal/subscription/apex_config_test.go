package subscription

import (
	"encoding/base64"
	"testing"
)

const testApexEnvelope = `{"format":"apex-config-v1","payload":"eyJkb21haW5zIjpbImh0dHBzOi8vYSJdfQ==","signature":"00"}`

// fl_apexv2.json is base64(XOR(envelope, key)) — obfuscation, not encryption.
// The XOR is positional, so the key length is load-bearing: a 31- or 33-byte key
// would decrypt into mojibake that still parses as UTF-8.
func TestDecodeApexConfigV2(t *testing.T) {
	key := []byte(apexConfigXORKey)
	if len(key) != 32 {
		t.Fatalf("apexConfigXORKey is %d bytes, want 32", len(key))
	}
	plain := []byte(testApexEnvelope)
	xored := make([]byte, len(plain))
	for i := range plain {
		xored[i] = plain[i] ^ key[i%len(key)]
	}
	body := []byte(base64.StdEncoding.EncodeToString(xored))

	got, err := DecodeApexConfig(body, true)
	if err != nil {
		t.Fatalf("DecodeApexConfig: %v", err)
	}
	if string(got) != testApexEnvelope {
		t.Fatalf("decoded = %q, want %q", got, testApexEnvelope)
	}
}

// The v1 file is the envelope itself; only the surrounding whitespace goes away.
func TestDecodeApexConfigV1PassesThrough(t *testing.T) {
	got, err := DecodeApexConfig([]byte("\n"+testApexEnvelope+"\n"), false)
	if err != nil {
		t.Fatalf("DecodeApexConfig: %v", err)
	}
	if string(got) != testApexEnvelope {
		t.Fatalf("decoded = %q, want %q", got, testApexEnvelope)
	}
}

func TestDecodeApexConfigRejectsGarbage(t *testing.T) {
	if _, err := DecodeApexConfig([]byte("this is not base64 at all!!"), true); err == nil {
		t.Fatal("expected an error for a non-base64 body")
	}
	if _, err := DecodeApexConfig(nil, false); err == nil {
		t.Fatal("expected an error for an empty body")
	}
}
