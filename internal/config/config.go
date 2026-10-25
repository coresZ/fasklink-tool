package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
)

const CurrentSchema = 2

// Proxy modes for outbound panel requests. The default is direct: the panel and
// its CDN are reachable without a proxy, and the system proxy (when one is set)
// costs several hundred milliseconds per request because every step opens a new
// connection through it.
const (
	ProxyDirect = "direct"
	ProxySystem = "system"
)

type Config struct {
	SchemaVersion int      `json:"schemaVersion,omitempty"`
	Host          string   `json:"host"`
	Port          int      `json:"port"`
	AccessKey     string   `json:"accessKey"`
	Domains       []string `json:"domains"`
	LogLevel      string   `json:"logLevel,omitempty"`
	// ProxyMode selects how outbound requests to the panel and its CDN are
	// routed: ProxyDirect ignores the system proxy, ProxySystem honours it.
	ProxyMode string `json:"proxyMode,omitempty"`
}

func Default() Config {
	return Config{
		SchemaVersion: CurrentSchema,
		Host:          "0.0.0.0",
		Port:          8787,
		AccessKey:     "fastlink",
		Domains:       []string{},
		LogLevel:      "info",
		ProxyMode:     ProxyDirect,
	}
}

// Path returns <root>/.run/config.json.
func Path(root string) string {
	return filepath.Join(root, ".run", "config.json")
}

// Load reads config.json and applies safe defaults. It creates the .run
// directory and writes a default file only when the file does not exist.
func Load(root string) (Config, error) {
	path := Path(root)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return Config{}, err
	}

	cfg := Default()
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		if err := Save(root, cfg); err != nil {
			return Config{}, err
		}
		return cfg, nil
	}
	if err != nil {
		return Config{}, err
	}
	raw = bytes.TrimPrefix(raw, []byte{0xEF, 0xBB, 0xBF})
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return Config{}, fmt.Errorf("config.json: %w", err)
	}
	if cfg.SchemaVersion == 0 {
		cfg.SchemaVersion = CurrentSchema
	}
	if cfg.Host != "0.0.0.0" && cfg.Host != "127.0.0.1" {
		cfg.Host = "0.0.0.0"
	}
	if cfg.Port < 1024 || cfg.Port > 65535 {
		cfg.Port = 8787
	}
	if strings.TrimSpace(cfg.AccessKey) == "" {
		cfg.AccessKey = "fastlink"
	}
	if cfg.ProxyMode != ProxySystem {
		cfg.ProxyMode = ProxyDirect
	}
	if cfg.Domains == nil {
		cfg.Domains = []string{}
	}
	return cfg, nil
}

func Save(root string, cfg Config) error {
	path := Path(root)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// SeedDomains adds the first non-loopback IPv4 when domains is empty.
func SeedDomains(cfg *Config) bool {
	if len(cfg.Domains) > 0 {
		return false
	}
	for _, ip := range LANAddresses() {
		cfg.Domains = []string{ip}
		return true
	}
	return false
}

func LANAddresses() []string {
	var out []string
	ifaces, err := net.Interfaces()
	if err != nil {
		return out
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ipNet, ok := addr.(*net.IPNet)
			if !ok || ipNet.IP.To4() == nil {
				continue
			}
			ip := ipNet.IP.String()
			if ip == "" || strings.HasPrefix(ip, "169.254.") {
				continue
			}
			out = append(out, ip)
		}
	}
	return unique(out)
}

func unique(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, v := range in {
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}
