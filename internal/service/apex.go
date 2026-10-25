package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
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
	apexConfigPath     = "/apos/fl_apex.json"
	apexAuthPath       = "/v2/auth/token"
	apexFeedPath       = "/v2/feed/access-token"
	apexDownloadPath   = "/v2/feed/download?access_key="
	apexSubscribeMapKey = "/api/v1/client/subscribe?token="
)

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

func (c ApexConfig) userAgent() string {
	if c.Version == "" {
		return "apex/" + subscription.ApexDefaultVersion()
	}
	return "apex/" + c.Version
}

// LoadApexConfig fetches fl_apex.json from the first reachable CDN host.
//
// The User-Agent must be lowercase `apex/<version>`. An uppercase one is answered
// with 403 by the CDN's Cloudflare layer, and a browser/Dart/okhttp UA gets 403 as
// well, which makes the endpoint look dead when it is not.
func LoadApexConfig(ctx context.Context, timeout time.Duration) (ApexConfig, error) {
	return loadApexConfig(ctx, timeout, subscription.ProxyDirect)
}

func loadApexConfig(ctx context.Context, timeout time.Duration, proxyMode string) (ApexConfig, error) {
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	transport := &http.Transport{
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
		target := strings.TrimRight(base, "/") + apexConfigPath
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
		if err != nil {
			errs = append(errs, err.Error())
			continue
		}
		req.Header.Set("User-Agent", "apex/"+subscription.ApexDefaultVersion())
		resp, err := client.Do(req)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", base, err))
			continue
		}
		body, readErr := subscription.ReadCapped(resp)
		resp.Body.Close()
		if readErr != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", base, readErr))
			continue
		}
		if resp.StatusCode != http.StatusOK {
			errs = append(errs, fmt.Sprintf("%s: HTTP %d", base, resp.StatusCode))
			continue
		}
		cfg, parseErr := parseApexConfig(body)
		if parseErr != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", base, parseErr))
			continue
		}
		if len(cfg.Domains) == 0 {
			errs = append(errs, fmt.Sprintf("%s: 配置里没有 domains", base))
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
		return resolved, nil
	}
	return ApexConfig{}, fmt.Errorf("没有可用的 Apex 配置源: %s", strings.Join(errs, "; "))
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
	cfg, err := loadApexConfig(ctx, 15*time.Second, s.proxyMode())
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
	AuthData  string
	AccessKey string
	Feed      map[string]any
}

func (s *Service) apexSession(ctx context.Context, cfg ApexConfig, email, password string) (*apexSession, error) {
	transport, err := s.apexTransport("api")
	if err != nil {
		return nil, err
	}
	apiClient := &http.Client{Timeout: 20 * time.Second, Transport: transport}
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
			errs = append(errs, fmt.Sprintf("%s: HTTP %d", base, resp.StatusCode))
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
		session := &apexSession{AuthData: payload.Data.AuthData, AccessKey: payload.Data.Token}

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

// apexDownload fetches the APXS container and decrypts it.
func (s *Service) apexDownload(ctx context.Context, cfg ApexConfig, accessKey string) (string, subscription.ApexHeader, string, error) {
	transport, err := s.apexTransport("subscription")
	if err != nil {
		return "", subscription.ApexHeader{}, "", err
	}
	subClient := &http.Client{Timeout: 25 * time.Second, Transport: transport}
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
		target := base + cfg.DownloadPath + separator + accessKey
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
			errs = append(errs, fmt.Sprintf("%s: HTTP %d", base, resp.StatusCode))
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
	raw, header, downloadURL, err := s.apexDownload(ctx, cfg, session.AccessKey)
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
