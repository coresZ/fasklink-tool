package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"fastlink-tool/internal/config"
	"fastlink-tool/internal/model"
	"fastlink-tool/internal/subscription"
)

// Apex / Speed panels do not expose /api/v1 at all. Their API lives under /v2,
// the route names are different, and every call needs a client certificate:
//
//	POST {api}/v2/auth/token              -> data.auth_data (JWT), data.token (access key)
//	GET  {api}/v2/feed/access-token       -> plan / traffic / expiry for this account
//	GET  {sub}/v2/feed/download?access_key= -> APXS v2 container
//
// The old /api/v1/client/subscribe path still answers on such panels, but it
// returns the provider's SSPanel-Malio compatibility layer: vmess/ss entries
// pointing at public IPs, all sharing the account uuid. Those are not the real
// nodes, so they must not be preferred.
const (
	apexAuthPath        = "/v2/auth/token"
	apexFeedPath        = "/v2/feed/access-token"
	apexDownloadPath    = "/v2/feed/download?access_key="
	apexSubscribeMapKey = "/api/v1/client/subscribe?token="
)

// apexConfigFile is one remote-config file and how its body is encoded.
type apexConfigFile struct {
	Path string
	// Obfuscated marks fl_apexv2.json: base64 of the envelope JSON XORed with a
	// fixed key, served under a Content-Type that claims plain JSON.
	Obfuscated bool
}

// apexConfigFiles is probed in order on every CDN host.
//
// v2 comes first because it is the file the 7.3.x client reads and the one the
// provider keeps current (v2 last-modified 2026-11-13, v1 2026-10-27). Both
// carry the same envelope and the same endpoints today; the difference that
// matters is `update.latest.*.version`, where v1 still says 7.2.5 and v2 says
// 7.3.1.
var apexConfigFiles = []apexConfigFile{
	{Path: subscription.ApexConfigPathV2, Obfuscated: true},
	{Path: subscription.ApexConfigPathV1},
}

// apexConfig mirrors the fields of fl_apex.json the backend needs. The file is
// base64(JSON) wrapped in an envelope, signed with Ed25519 (the signature is not
// verified: it protects the CDN copy, and the panel endpoints below are what the
// data is actually used for).
type apexConfig struct {
	EnableAPIPathMapping bool              `json:"enable_api_path_mapping"`
	SubscribeDomain      []string          `json:"subscribe_domain"`
	SubscribePath        map[string]string `json:"subscribe_path"`
	PanelType            string            `json:"panel_type"`
	Domains              []string          `json:"domains"`
	Update               struct {
		Latest map[string]struct {
			Version string `json:"version"`
		} `json:"latest"`
	} `json:"update"`
}

// apexConfigSources are the CDN hosts that still serve fl_apex.json. The APK
// ships the same hosts with an `-ax.` infix; that generation is offline (404,
// expired certificate, or 500), while the infix-free names serve the same file.
var apexConfigSources = []string{
	"https://cdno01.llguanglisf.com",
	"https://apisa.cnossfile.com",
	"https://tcdn.getxlx.com",
}

// ApexConfig is the resolved remote configuration, exposed to the UI so the
// settings page can show where the endpoints came from.
type ApexConfig struct {
	Source       string
	Version      string
	APIBases     []string
	SubBases     []string
	DownloadPath string
	PanelType    string
}

// apexClientVersion picks the version the User-Agent reports. The CDN config
// advertises the latest *published* build, which trails the APK the certificate
// and key material were taken from (2026-11-14: config says 7.2.5, the APK in
// circulation is 7.3.1), so the newer of the two wins. Reporting a version older
// than the material the request is signed with is the one combination the panel
// has never had to accept.
func apexClientVersion(configVersion string) string {
	configVersion = strings.TrimSpace(configVersion)
	if configVersion == "" {
		return subscription.ApexDefaultVersion()
	}
	return newerVersion(configVersion, subscription.ApexDefaultVersion())
}

func (c ApexConfig) userAgent() string {
	return subscription.ApexUserAgent(apexClientVersion(c.Version))
}

// newerVersion returns the higher of two dotted version strings.
func newerVersion(a, b string) string {
	if versionLess(a, b) {
		return b
	}
	return a
}

func versionLess(a, b string) bool {
	left, right := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(left) || i < len(right); i++ {
		var x, y int
		if i < len(left) {
			x, _ = strconv.Atoi(strings.TrimSpace(left[i]))
		}
		if i < len(right) {
			y, _ = strconv.Atoi(strings.TrimSpace(right[i]))
		}
		if x != y {
			return x < y
		}
	}
	return false
}

// LoadApexConfig fetches fl_apex.json from the first reachable CDN host.
//
// The request carries the client's own User-Agent, `ua.net.flclient.app.apex/v<version>`;
// `apex/<version>` is retried as a fallback. An uppercase `Apex/` is answered with
// 403 by the CDN's Cloudflare layer, and a browser/Dart/okhttp UA gets 403 as
// well, which makes the endpoint look dead when it is not.
func LoadApexConfig(ctx context.Context, timeout time.Duration) (ApexConfig, error) {
	return loadApexConfig(ctx, timeout, subscription.ProxyDirect)
}

func loadApexConfig(ctx context.Context, timeout time.Duration, proxyMode string) (ApexConfig, error) {
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	transport := &http.Transport{
		// The CDN hosts share the panel's round-robin shape: several records, most
		// of them dead from here. Race them the same way the panel transports do.
		DialContext:         subscription.ApexDialContext,
		TLSHandshakeTimeout: 10 * time.Second,
		MaxIdleConns:        4,
		MaxIdleConnsPerHost: 2,
		IdleConnTimeout:     90 * time.Second,
		ForceAttemptHTTP2:   true,
	}
	if proxyMode == subscription.ProxySystem {
		transport.Proxy = http.ProxyFromEnvironment
	}
	client := &http.Client{Timeout: timeout, Transport: transport}
	defer transport.CloseIdleConnections()
	var errs []string
	for _, base := range apexConfigSources {
		origin := strings.TrimRight(base, "/")
		var lastErr error = errors.New("没有可用的配置文件")
		for _, file := range apexConfigFiles {
			target := origin + file.Path
			for _, ua := range apexConfigUserAgents() {
				body, status, fetchErr := fetchApexConfig(ctx, client, target, ua)
				if fetchErr != nil {
					lastErr = fmt.Errorf("%s: %w", file.Path, fetchErr)
					continue
				}
				if status != http.StatusOK {
					lastErr = fmt.Errorf("%s: HTTP %d", file.Path, status)
					continue
				}
				envelope, decodeErr := subscription.DecodeApexConfig(body, file.Obfuscated)
				if decodeErr != nil {
					lastErr = fmt.Errorf("%s: %w", file.Path, decodeErr)
					continue
				}
				cfg, parseErr := parseApexConfig(envelope)
				if parseErr != nil {
					lastErr = fmt.Errorf("%s: %w", file.Path, parseErr)
					continue
				}
				if len(cfg.Domains) == 0 {
					lastErr = fmt.Errorf("%s: 配置里没有 domains", file.Path)
					continue
				}
				resolved := ApexConfig{
					Source:       target,
					PanelType:    cfg.PanelType,
					APIBases:     trimAll(cfg.Domains),
					SubBases:     trimAll(cfg.SubscribeDomain),
					DownloadPath: apexDownloadPath,
				}
				if latest, ok := cfg.Update.Latest["android"]; ok && latest.Version != "" {
					resolved.Version = latest.Version
				}
				if mapped := cfg.SubscribePath[apexSubscribeMapKey]; mapped != "" {
					resolved.DownloadPath = mapped
				}
				if len(resolved.SubBases) == 0 {
					resolved.SubBases = resolved.APIBases
				}
				slog.Debug("apex config loaded",
					"source", resolved.Source, "panel", resolved.PanelType,
					"version", resolved.Version, "api", resolved.APIBases, "sub", resolved.SubBases)
				return resolved, nil
			}
		}
		errs = append(errs, fmt.Sprintf("%s: %v", origin, lastErr))
	}
	return ApexConfig{}, fmt.Errorf("没有可用的 Apex 配置源: %s", strings.Join(errs, "; "))
}

// apexConfigUserAgents are the User-Agent spellings the config CDN has answered
// to. The full client string is what the app sends; the short `apex/<version>`
// form is kept as a second attempt because the Cloudflare rule in front of the
// CDN has flipped between the two before, and a config fetch is the one request
// that cannot afford to guess wrong.
func apexConfigUserAgents() []string {
	version := subscription.ApexDefaultVersion()
	return []string{
		subscription.ApexUserAgent(version),
		"apex/" + version,
	}
}

// fetchApexConfig performs one config GET and returns the capped body.
func fetchApexConfig(ctx context.Context, client *http.Client, target, userAgent string) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	body, readErr := subscription.ReadCapped(resp)
	if readErr != nil {
		return nil, resp.StatusCode, readErr
	}
	return body, resp.StatusCode, nil
}

func parseApexConfig(body []byte) (apexConfig, error) {
	var envelope struct {
		Format  string `json:"format"`
		Payload string `json:"payload"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return apexConfig{}, fmt.Errorf("配置不是 JSON: %w", err)
	}
	if envelope.Payload == "" {
		return apexConfig{}, errors.New("配置缺少 payload")
	}
	raw, err := base64.StdEncoding.DecodeString(envelope.Payload)
	if err != nil {
		return apexConfig{}, fmt.Errorf("payload 不是 base64: %w", err)
	}
	var cfg apexConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return apexConfig{}, fmt.Errorf("payload 不是 JSON: %w", err)
	}
	return cfg, nil
}

func trimAll(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimRight(strings.TrimSpace(value), "/"); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

// apexConfig returns the cached remote config, fetching it when stale.
func (s *Service) apexConfig(ctx context.Context) (ApexConfig, error) {
	s.apexMu.Lock()
	defer s.apexMu.Unlock()
	if s.apexCfg.Source != "" && time.Since(s.apexCfgAt) < apexConfigTTL {
		return s.apexCfg, nil
	}
	cfg, err := loadApexConfig(ctx, 20*time.Second, s.proxyMode())
	if err != nil {
		return ApexConfig{}, err
	}
	s.apexCfg, s.apexCfgAt = cfg, time.Now()
	return cfg, nil
}

// invalidateApexConfig drops the cache so the next call refetches. Used when a
// request fails in a way that suggests the endpoints moved.
func (s *Service) invalidateApexConfig() {
	s.apexMu.Lock()
	s.apexCfg = ApexConfig{}
	s.apexCfgAt = time.Time{}
	s.apexMu.Unlock()
}

func (s *Service) proxyMode() string {
	if s.Config.ProxyMode == config.ProxySystem {
		return subscription.ProxySystem
	}
	return subscription.ProxyDirect
}

// apexTransport returns the shared transport for a purpose. Holding one keeps the
// TCP connection and TLS session alive across refreshes; a fresh transport per
// request cost a full mutual-TLS handshake every time.
func (s *Service) apexTransport(purpose string) (*http.Transport, error) {
	switch purpose {
	case "api":
		var err error
		s.apexAPIOnce.Do(func() { s.apexAPI, err = subscription.ApexTransport(purpose, s.proxyMode()) })
		if s.apexAPI == nil {
			return nil, err
		}
		return s.apexAPI, nil
	case "subscription":
		var err error
		s.apexSubOnce.Do(func() { s.apexSub, err = subscription.ApexTransport(purpose, s.proxyMode()) })
		if s.apexSub == nil {
			return nil, err
		}
		return s.apexSub, nil
	default:
		return subscription.ApexTransport(purpose, s.proxyMode())
	}
}

// ResetTransports drops the pooled connections so a proxy-mode change takes
// effect on the next request.
func (s *Service) ResetTransports() {
	s.apexMu.Lock()
	defer s.apexMu.Unlock()
	if s.apexAPI != nil {
		s.apexAPI.CloseIdleConnections()
	}
	if s.apexSub != nil {
		s.apexSub.CloseIdleConnections()
	}
	s.apexAPI, s.apexSub = nil, nil
	s.apexAPIOnce, s.apexSubOnce = sync.Once{}, sync.Once{}
	s.apexCfg, s.apexCfgAt = ApexConfig{}, time.Time{}
}

// apexSession runs the /v2 login flow and returns the access key plus the account
// record the panel reports.
type apexSession struct {
	// Base is the API origin that answered the login; the feed endpoint has to be
	// asked on the same one.
	Base      string
	AuthData  string
	AccessKey string
	Feed      map[string]any
}

func (s *Service) apexSession(ctx context.Context, cfg ApexConfig, email, password string) (*apexSession, error) {
	transport, err := s.apexTransport("api")
	if err != nil {
		return nil, err
	}
	// The panel sits behind Cloudflare and answers in ~0.5 s when healthy, but it
	// has been seen to hold the connection for >20 s while a challenge or an
	// upstream reroute settles. A timeout shorter than the server's own patience
	// turns a slow-but-working login into a hard failure.
	apiClient := &http.Client{Timeout: 40 * time.Second, Transport: transport}
	if s.Transport != nil {
		apiClient.Transport = s.Transport
	}
	ua := cfg.userAgent()

	var errs []string
	for _, base := range cfg.APIBases {
		form := url.Values{"email": {email}, "password": {password}}
		req, reqErr := http.NewRequestWithContext(ctx, http.MethodPost, base+apexAuthPath, strings.NewReader(form.Encode()))
		if reqErr != nil {
			errs = append(errs, reqErr.Error())
			continue
		}
		req.Header.Set("User-Agent", ua)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		resp, doErr := apiClient.Do(req)
		if doErr != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", base, doErr))
			continue
		}
		body, readErr := subscription.ReadCapped(resp)
		resp.Body.Close()
		if readErr != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", base, readErr))
			continue
		}
		if resp.StatusCode != http.StatusOK {
			errs = append(errs, fmt.Sprintf("%s: %v", base, subscription.ErrorFromResponse(resp, body)))
			continue
		}
		var payload struct {
			Data struct {
				AuthData string `json:"auth_data"`
				Token    string `json:"token"`
			} `json:"data"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			errs = append(errs, fmt.Sprintf("%s: 登录返回非 JSON", base))
			continue
		}
		if payload.Data.AuthData == "" || payload.Data.Token == "" {
			errs = append(errs, fmt.Sprintf("%s: 登录返回缺少 auth_data/token", base))
			continue
		}
		session := &apexSession{Base: base, AuthData: payload.Data.AuthData, AccessKey: payload.Data.Token}

		feedReq, _ := http.NewRequestWithContext(ctx, http.MethodGet, base+apexFeedPath, nil)
		feedReq.Header.Set("User-Agent", ua)
		feedReq.Header.Set("Authorization", session.AuthData)
		feedResp, feedErr := apiClient.Do(feedReq)
		if feedErr == nil {
			feedBody, _ := io.ReadAll(io.LimitReader(feedResp.Body, subscription.MaxProfileBytes))
			feedResp.Body.Close()
			if feedResp.StatusCode == http.StatusOK {
				var feed struct {
					Data map[string]any `json:"data"`
				}
				if json.Unmarshal(feedBody, &feed) == nil {
					session.Feed = feed.Data
				}
			}
		}
		return session, nil
	}
	return nil, fmt.Errorf("Apex 登录失败: %s", strings.Join(errs, "; "))
}

// apexSessionKey mints a fresh access key for an existing login. The feed
// endpoint rotates the key on every call, so asking immediately before each
// download is what keeps a single-use key valid for exactly one request.
func (s *Service) apexSessionKey(ctx context.Context, cfg ApexConfig, session *apexSession) (string, error) {
	if session == nil || session.AuthData == "" || session.Base == "" {
		return "", errors.New("没有可用的登录会话")
	}
	transport, err := s.apexTransport("api")
	if err != nil {
		return "", err
	}
	client := &http.Client{Timeout: 40 * time.Second, Transport: transport}
	if s.Transport != nil {
		client.Transport = s.Transport
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, session.Base+apexFeedPath, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", cfg.userAgent())
	req.Header.Set("Authorization", session.AuthData)
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	body, readErr := subscription.ReadCapped(resp)
	resp.Body.Close()
	if readErr != nil {
		return "", readErr
	}
	if resp.StatusCode != http.StatusOK {
		return "", subscription.ErrorFromResponse(resp, body)
	}
	var payload struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", errors.New("订阅凭证返回非 JSON")
	}
	if payload.Data.Token == "" {
		return "", errors.New("订阅凭证返回缺少 token")
	}
	return payload.Data.Token, nil
}

// apexDownload fetches the APXS container and decrypts it.
//
// The access key is single-use on the current panel: the first request through
// /v2/feed/download invalidates it, and that includes an attempt that dies at a
// CDN edge (the next host then answers 403 "invalid token"). Testing SubBases in
// order therefore has to mint a key per host, which is why this takes the
// session instead of a key string.
func (s *Service) apexDownload(ctx context.Context, cfg ApexConfig, session *apexSession) (string, subscription.ApexHeader, string, error) {
	if session == nil {
		return "", subscription.ApexHeader{}, "", errors.New("没有可用的登录会话")
	}
	transport, err := s.apexTransport("subscription")
	if err != nil {
		return "", subscription.ApexHeader{}, "", err
	}
	// Same reasoning as the login client: the download is a single 34 KB body and
	// normally returns in under a second, so a 45 s budget only ever fires on a
	// genuinely stalled connection.
	subClient := &http.Client{Timeout: 45 * time.Second, Transport: transport}
	if s.Transport != nil {
		subClient.Transport = s.Transport
	}
	ua := cfg.userAgent()
	separator := ""
	if !strings.HasSuffix(cfg.DownloadPath, "=") && !strings.HasSuffix(cfg.DownloadPath, "?") {
		separator = "="
	}

	var errs []string
	for _, base := range cfg.SubBases {
		key := session.AccessKey
		if fresh, keyErr := s.apexSessionKey(ctx, cfg, session); keyErr == nil && fresh != "" {
			key = fresh
		} else if key == "" {
			errs = append(errs, fmt.Sprintf("%s: 没有订阅凭证: %v", base, keyErr))
			continue
		}
		target := base + cfg.DownloadPath + separator + key
		req, reqErr := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
		if reqErr != nil {
			errs = append(errs, reqErr.Error())
			continue
		}
		req.Header.Set("User-Agent", ua)
		resp, doErr := subClient.Do(req)
		if doErr != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", base, doErr))
			continue
		}
		body, readErr := subscription.ReadCapped(resp)
		resp.Body.Close()
		if readErr != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", base, readErr))
			continue
		}
		if resp.StatusCode != http.StatusOK {
			errs = append(errs, fmt.Sprintf("%s: %v", base, subscription.ErrorFromResponse(resp, body)))
			continue
		}
		if !subscription.IsAPXS(body) {
			// A plain base64 share list here means the panel fell back to the
			// generic subscription, i.e. Apex.php is not deployed.
			errs = append(errs, fmt.Sprintf("%s: 不是 APXS 数据（开头 %q）", base, headOf(body, 8)))
			continue
		}
		yamlText, header, decryptErr := subscription.DecryptApexProfile(body)
		if decryptErr != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", base, decryptErr))
			continue
		}
		return yamlText, header, target, nil
	}
	return "", subscription.ApexHeader{}, "", fmt.Errorf("Apex 订阅下载失败: %s", strings.Join(errs, "; "))
}

func headOf(body []byte, n int) string {
	if len(body) < n {
		n = len(body)
	}
	return string(body[:n])
}

// LoginApex performs the full Apex flow for one account and returns the profile
// plus the account metadata the panel reported.
func (s *Service) LoginApex(ctx context.Context, account model.Account) (*LoginResult, error) {
	password, err := s.Protector.Decrypt(account.PasswordEnc)
	if err != nil {
		return nil, fmt.Errorf("密码解密失败: %w", err)
	}
	if password == "" {
		return nil, errors.New("该账号缺少邮箱或密码，请重新填写")
	}
	email := strings.TrimSpace(account.Email)
	if email == "" {
		return nil, errors.New("该账号缺少邮箱")
	}

	cfg, err := s.apexConfig(ctx)
	if err != nil {
		return nil, err
	}
	session, err := s.apexSession(ctx, cfg, email, password)
	if err != nil {
		return nil, err
	}
	raw, header, downloadURL, err := s.apexDownload(ctx, cfg, session)
	if err != nil {
		// The endpoints may have moved; refetch the config next time instead of
		// retrying the same stale domains until the TTL expires.
		s.invalidateApexConfig()
		return nil, err
	}
	yamlText, err := subscription.ApplyTemplate(raw)
	if err != nil {
		return nil, err
	}
	result := &LoginResult{
		SubscriptionURL: downloadURL,
		YAML:            yamlText,
		RawYAML:         raw,
		UserInfo:        apexUserInfo(session.Feed),
		Apex:            &header,
	}
	_ = s.Store.UpdateAccountRuntime(account.ID, result.SubscriptionURL, time.Now().Unix(), 0, result.UserInfo, "")
	return result, nil
}

// apexUserInfo maps /v2/feed/access-token onto the same traffic block the rest
// of the UI renders, so Apex accounts show usage and expiry like the others.
func apexUserInfo(feed map[string]any) *model.UserInfo {
	if feed == nil {
		return nil
	}
	num := func(key string) int64 {
		switch v := feed[key].(type) {
		case float64:
			return int64(v)
		case int64:
			return v
		case json.Number:
			n, _ := v.Int64()
			return n
		}
		return 0
	}
	info := &model.UserInfo{
		Upload:   num("u"),
		Download: num("d"),
		Total:    num("transfer_enable"),
		Expire:   num("expired_at"),
	}
	if info.Total == 0 && info.Expire == 0 {
		return nil
	}
	return info
}
