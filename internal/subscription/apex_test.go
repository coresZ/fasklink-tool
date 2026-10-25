package subscription

import (
	"encoding/hex"
	"strings"
	"testing"
)

const apexTestYAML = "mixed-port: 7890\nproxies:\n  - name: test-node\n    type: vless\n    server: example.com\n"

func apexTestKey(t *testing.T) []byte {
	t.Helper()
	key, err := hex.DecodeString(apexAESKeyHex)
	if err != nil {
		t.Fatalf("apexAESKeyHex: %v", err)
	}
	if len(key) != 32 {
		t.Fatalf("key length = %d, want 32", len(key))
	}
	return key
}

func TestDecryptApexProfileRoundTrip(t *testing.T) {
	blob, err := BuildAPXS([]byte(apexTestYAML), apexTestKey(t))
	if err != nil {
		t.Fatal(err)
	}
	got, header, err := DecryptApexProfile(blob)
	if err != nil {
		t.Fatal(err)
	}
	if got != apexTestYAML {
		t.Fatalf("plaintext = %q", got)
	}
	if header.Version != 2 || header.KeyID != 1 || header.EncVersion != 1 {
		t.Fatalf("header = %+v", header)
	}
}

func TestIsAPXS(t *testing.T) {
	blob, err := BuildAPXS([]byte(apexTestYAML), apexTestKey(t))
	if err != nil {
		t.Fatal(err)
	}
	if !IsAPXS(blob) {
		t.Fatal("IsAPXS rejected a real container")
	}
	for _, notAPXS := range [][]byte{
		nil,
		[]byte("proxies:\n"),
		[]byte("APXS"),                       // too short
		[]byte("APXS\x02"),                   // header incomplete
		[]byte("XXXX\x02\x00\x00\x00\x01\x00"), // wrong magic
	} {
		if IsAPXS(notAPXS) {
			t.Fatalf("IsAPXS accepted %q", notAPXS)
		}
	}
}

func TestParseApexHeaderRejects(t *testing.T) {
	cases := map[string][]byte{
		"short":       []byte("APXS"),
		"bad magic":   []byte("APXSX"),
		"wrong magic": append([]byte("ZZZZ"), make([]byte, 16)...),
		"version 1":   append([]byte("APXS\x01"), make([]byte, 16)...),
		"version 3":   append([]byte("APXS\x03"), make([]byte, 16)...),
	}
	for name, raw := range cases {
		if _, err := ParseApexHeader(raw); err == nil {
			t.Errorf("%s: expected error, got nil", name)
		}
	}
}

// A payload encrypted with a different key must not be mistaken for a config.
func TestDecryptApexProfileWrongKey(t *testing.T) {
	wrong := make([]byte, 32)
	blob, err := BuildAPXS([]byte(apexTestYAML), wrong)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := DecryptApexProfile(blob); err == nil {
		t.Fatal("expected an error when decrypting with the wrong key")
	}
}

// The container's 80-byte trailer is binary; it must be dropped, not decoded.
func TestDecryptApexProfileDropsTrailer(t *testing.T) {
	blob, err := BuildAPXS([]byte(apexTestYAML), apexTestKey(t))
	if err != nil {
		t.Fatal(err)
	}
	// Fill the trailer with bytes that are invalid UTF-8 continuation bytes.
	for i := len(blob) - apexTrailer; i < len(blob); i++ {
		blob[i] = 0x80
	}
	got, _, err := DecryptApexProfile(blob)
	if err != nil {
		t.Fatal(err)
	}
	if got != apexTestYAML {
		t.Fatalf("plaintext = %q", got)
	}
}

// A truncated body must be reported, not panic on slicing.
func TestDecryptApexProfileTruncated(t *testing.T) {
	blob, err := BuildAPXS([]byte(apexTestYAML), apexTestKey(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, cut := range []int{apexHeaderLen, apexHeaderLen + apexNonceLen, apexHeaderLen + apexNonceLen + apexTrailer} {
		if _, _, err := DecryptApexProfile(blob[:cut]); err == nil {
			t.Errorf("truncated at %d: expected an error", cut)
		}
	}
}

// DecryptProfile must recognise APXS without the caller having to check first.
func TestDecryptProfileDispatchesAPXS(t *testing.T) {
	blob, err := BuildAPXS([]byte(apexTestYAML), apexTestKey(t))
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecryptProfile(blob)
	if err != nil {
		t.Fatal(err)
	}
	if got != apexTestYAML {
		t.Fatalf("plaintext = %q", got)
	}
}

// The older scheme must keep working: adding APXS must not shadow it.
// DecryptProfile trims surrounding whitespace on the pass-through paths, so the
// fixtures carry a trailing newline and the assertion compares the trimmed form.
func TestDecryptProfileStillHandlesLegacy(t *testing.T) {
	yaml := "proxies:\n  - name: legacy\n    type: ss\n    server: example.com\n"
	want := strings.TrimSpace(yaml)
	if got, err := DecryptProfile([]byte(yaml)); err != nil || got != want {
		t.Fatalf("raw YAML: got %q, err %v", got, err)
	}
	if got, err := DecryptProfile([]byte(encryptGCMTest(yaml))); err != nil || got != yaml {
		t.Fatalf("AES-GCM: got %q, err %v", got, err)
	}
	if got, err := DecryptProfile([]byte(obfuscateTest(yaml))); err != nil || got != yaml {
		t.Fatalf("obfuscated base64: got %q, err %v", got, err)
	}
}

// Every embedded certificate must parse and pair with its key, otherwise the
// panel answers 404 and the failure looks like a dead endpoint.
func TestApexCertificatesParse(t *testing.T) {
	for _, purpose := range ApexPurposes() {
		client, err := ApexHTTPClient(purpose, 0)
		if err != nil {
			t.Errorf("%s: %v", purpose, err)
			continue
		}
		if client.Transport == nil {
			t.Errorf("%s: nil transport", purpose)
		}
	}
	if _, err := ApexHTTPClient("nosuch", 0); err == nil {
		t.Error("expected an error for an unknown purpose")
	}
}

// The three purposes must use distinct certificates, otherwise the panel's
// per-purpose routing would be pointless.
func TestApexProfilesAreDistinct(t *testing.T) {
	profiles := apexProfiles()
	seen := map[string]string{}
	for purpose, profile := range profiles {
		if strings.TrimSpace(profile.CertPEM) == "" || strings.TrimSpace(profile.KeyPEM) == "" {
			t.Fatalf("%s: empty PEM", purpose)
		}
		if prev, ok := seen[profile.CertPEM]; ok {
			t.Fatalf("%s and %s share a certificate", purpose, prev)
		}
		seen[profile.CertPEM] = purpose
	}
	if len(profiles) != len(ApexPurposes()) {
		t.Fatalf("profiles = %d, purposes = %d", len(profiles), len(ApexPurposes()))
	}
}
