package secret

import (
	"runtime"
	"testing"
)

func TestDPAPIRoundTrip(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows only")
	}
	p := New(t.TempDir())
	const plain = "密码-test-123"
	enc, err := p.Encrypt(plain)
	if err != nil {
		t.Fatal(err)
	}
	if len(enc) < 10 || enc[:6] != "dpapi:" {
		t.Fatalf("encrypted value has unexpected prefix: %q", enc[:min(20, len(enc))])
	}
	got, err := p.Decrypt(enc)
	if err != nil {
		t.Fatal(err)
	}
	if got != plain {
		t.Fatalf("got %q", got)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
