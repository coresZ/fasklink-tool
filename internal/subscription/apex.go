package subscription

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/tls"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"time"
	"unicode/utf8"
)

// readRandom fills buf from crypto/rand. Wrapped so tests can stub it.
var readRandom = rand.Read

// APXS v2 is the container the Apex / Speed client receives from
// /v2/feed/download:
//
//	offset  len  field
//	0       4    "APXS"
//	4       1    version (2)
//	5       4    keyId       u32 big-endian, a table index, not a key
//	9       4    encVersion  u32 big-endian
//	13      12   nonce
//	25      N    AES-256-GCM ciphertext
//	25+N    80   trailer (16B check + 64B signature, outside the YAML body)
//
// The key is a 32-byte constant compiled into libapp.so; see apex_keys.go.
const (
	apexMagic     = "APXS"
	apexHeaderLen = 13
	apexNonceLen  = 12
	apexTrailer   = 80
)

// ApexHeader is the parsed container prefix, surfaced so the UI can show which
// key generation a payload used.
type ApexHeader struct {
	Version    int
	KeyID      uint32
	EncVersion uint32
	// TrailerLen records how many bytes followed the YAML body. The panel appends
	// 80 bytes that are neither part of the YAML nor needed to decrypt it.
	TrailerLen int
}

type apexProfile struct {
	CertPEM string
	KeyPEM  string
}

// IsAPXS reports whether raw looks like an APXS container. Callers use it to pick
// between DecryptApexProfile and the older DecryptProfile.
func IsAPXS(raw []byte) bool {
	return len(raw) >= apexHeaderLen && string(raw[:4]) == apexMagic
}

// ParseApexHeader validates the container prefix. A wrong magic or an unknown
// version is reported verbatim: the client's own diagnostics distinguish
// "Apex.php not deployed" (generic base64) from "key mismatch" (real ciphertext).
func ParseApexHeader(raw []byte) (ApexHeader, error) {
	if len(raw) < apexHeaderLen {
		return ApexHeader{}, fmt.Errorf("APXS 数据过短: %d 字节", len(raw))
	}
	if string(raw[:4]) != apexMagic {
		return ApexHeader{}, fmt.Errorf("不是 APXS 容器: 开头是 %q", string(raw[:4]))
	}
	header := ApexHeader{
		Version:    int(raw[4]),
		KeyID:      binary.BigEndian.Uint32(raw[5:9]),
		EncVersion: binary.BigEndian.Uint32(raw[9:13]),
	}
	if header.Version != 2 {
		return ApexHeader{}, fmt.Errorf("不支持的 APXS 版本: %d（仅支持 2）", header.Version)
	}
	return header, nil
}

// DecryptApexProfile decodes an APXS v2 payload into the Clash YAML it carries.
//
// The GCM tag is deliberately not verified. This server appends a 16-byte check
// plus a 64-byte signature, but the tag does not sit where the standard
// nonce|ciphertext|tag split puts it, so gcm.Open() rejects the correct key.
// The plaintext is validated instead: it must decode as UTF-8 and look like a
// Clash config, which a wrong key cannot satisfy by accident.
func DecryptApexProfile(raw []byte) (string, ApexHeader, error) {
	header, err := ParseApexHeader(raw)
	if err != nil {
		return "", ApexHeader{}, err
	}
	key, err := hex.DecodeString(apexAESKeyHex)
	if err != nil {
		return "", header, err
	}
	body := raw[apexHeaderLen:]
	if len(body) <= apexNonceLen+apexTrailer {
		return "", header, fmt.Errorf("APXS 正文过短: %d 字节", len(body))
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", header, err
	}
	nonce := body[:apexNonceLen]
	ciphertext := body[apexNonceLen : len(body)-apexTrailer]
	header.TrailerLen = apexTrailer

	plain := make([]byte, len(ciphertext))
	// GCM encrypts with CTR starting at counter 2; NewCTR with the same initial
	// block reproduces that keystream exactly.
	stream := cipher.NewCTR(block, gcmCounterBlock(nonce))
	stream.XORKeyStream(plain, ciphertext)

	text, dropped, err := trimToValidUTF8(plain)
	if err != nil {
		return "", header, err
	}
	header.TrailerLen += dropped
	if !IsClashYAML(text) {
		return "", header, errors.New("APXS 解密结果不是 Clash 配置，密钥可能已随客户端更新")
	}
	return text, header, nil
}

// gcmCounterBlock builds the initial counter block GCM uses for a 12-byte nonce:
// nonce || 0x00000002.
func gcmCounterBlock(nonce []byte) []byte {
	block := make([]byte, aes.BlockSize)
	copy(block, nonce)
	binary.BigEndian.PutUint32(block[aes.BlockSize-4:], 2)
	return block
}

// trimToValidUTF8 returns the longest prefix that decodes as UTF-8 plus the
// number of bytes dropped. The container's trailer is binary; letting it reach
// the YAML parser would turn a successful decrypt into a decode error.
func trimToValidUTF8(raw []byte) (string, int, error) {
	if len(raw) == 0 {
		return "", 0, errors.New("解密结果为空")
	}
	end := 0
	for end < len(raw) {
		r, size := utf8.DecodeRune(raw[end:])
		if r == utf8.RuneError && size == 1 {
			break
		}
		end += size
	}
	if end == 0 {
		return "", 0, errors.New("解密结果不是文本")
	}
	return string(raw[:end]), len(raw) - end, nil
}

// ApexHTTPClient returns a client presenting the client certificate for one
// purpose. The panel answers 404 (not 403) when the certificate is missing or
// belongs to a different purpose, which reads like a dead endpoint; that is why
// the three profiles are addressed explicitly instead of tried in turn.
//
// Each call builds a fresh transport, so it is only suitable for one-off use.
// Long-lived callers should hold a Transport instead: reusing one keeps the TCP
// connection and the TLS session, which matters because every Apex step is a
// separate mTLS handshake otherwise.
func ApexHTTPClient(purpose string, timeout time.Duration) (*http.Client, error) {
	transport, err := ApexTransport(purpose, ProxyDirect)
	if err != nil {
		return nil, err
	}
	return &http.Client{Timeout: timeout, Transport: transport}, nil
}

// Proxy modes for outbound requests.
const (
	// ProxyDirect ignores HTTP(S)_PROXY. This is the default because the panel
	// and its CDN are reachable without one, and routing through a local proxy
	// adds a few hundred milliseconds per request.
	ProxyDirect = "direct"
	// ProxySystem honours HTTP(S)_PROXY, for networks that can only reach the
	// panel through a proxy.
	ProxySystem = "system"
)

// ApexTransport builds a reusable transport carrying the certificate for one
// purpose. Reuse it across requests: a new transport forces a new TCP connection
// and a new mutual-TLS handshake every time.
func ApexTransport(purpose, proxyMode string) (*http.Transport, error) {
	profile, ok := apexProfiles()[purpose]
	if !ok {
		return nil, fmt.Errorf("未知的证书用途: %s", purpose)
	}
	cert, err := tls.X509KeyPair([]byte(profile.CertPEM), []byte(profile.KeyPEM))
	if err != nil {
		return nil, fmt.Errorf("加载 %s 证书失败: %w", purpose, err)
	}
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			Certificates: []tls.Certificate{cert},
			MinVersion:   tls.VersionTLS12,
			// The panel fronts a self-signed chain on some of its CDN hosts.
			InsecureSkipVerify: true,
		},
		MaxIdleConns:        8,
		MaxIdleConnsPerHost: 4,
		IdleConnTimeout:     90 * time.Second,
		ForceAttemptHTTP2:   true,
	}
	if proxyMode == ProxySystem {
		transport.Proxy = http.ProxyFromEnvironment
	}
	return transport, nil
}

// ApexPurposes lists the certificate purposes the app ships, for the settings
// page and for error messages.
func ApexPurposes() []string {
	return []string{"api", "oss", "subscription"}
}

// ApexDefaultVersion is the client version the embedded material was recovered
// from. The CDN answers only to `apex/<version>`; see LoadApexConfig.
func ApexDefaultVersion() string { return apexDefaultVersion }

// BuildAPXS assembles a container the way the panel does, with a zeroed trailer.
// Used by tests to round-trip a known payload.
func BuildAPXS(plain []byte, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, apexNonceLen)
	if _, err := readRandom(nonce); err != nil {
		return nil, err
	}
	ciphertext := make([]byte, len(plain))
	cipher.NewCTR(block, gcmCounterBlock(nonce)).XORKeyStream(ciphertext, plain)

	out := make([]byte, 0, apexHeaderLen+apexNonceLen+len(ciphertext)+apexTrailer)
	out = append(out, apexMagic...)
	out = append(out, 2)
	out = binary.BigEndian.AppendUint32(out, 1) // keyId
	out = binary.BigEndian.AppendUint32(out, 1) // encVersion
	out = append(out, nonce...)
	out = append(out, ciphertext...)
	out = append(out, make([]byte, apexTrailer)...)
	return out, nil
}
