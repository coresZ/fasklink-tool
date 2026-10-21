package service

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"

	"fastlink-tool/internal/config"
	"fastlink-tool/internal/model"
	"fastlink-tool/internal/secret"
	"fastlink-tool/internal/store"
	"fastlink-tool/internal/subscription"
)

const defaultAPIOrigin = "https://flesa.apiv2.a047.com"

type Service struct {
	Root      string
	Config    config.Config
	Store     *store.Store
	Protector *secret.Protector
	Fetcher   *subscription.Fetcher
	Transport http.RoundTripper

	mu      sync.Mutex
	probe   Probe
	refresh bool
}

type Probe struct {
	At       int64
	Nodes    int
	YAML     string
	UserInfo *model.UserInfo
	Error    string
	Source   string
	Running  bool
}

type LoginResult struct {
	SubscriptionURL string
	YAML            string
	// RawYAML is the subscription exactly as the provider decrypted it, before the
	// Clash template was applied. Kept so the decrypt tool can show both variants.
	RawYAML  string
	UserInfo *model.UserInfo
}

type resolvedSource struct {
	model.Source
	Fresh *LoginResult
}

type StateData struct {
	Healthy    bool
	Nodes      int
	UserInfo   *model.UserInfo
	ProbeError string
	Pending    bool
	SubURL     string
	NodeList   []model.Node
	Addresses  []model.Address
	Active     string
	Label      string
	FetchedAt  int64
	Cached     bool
}

type AccountInput struct {
	ID              string
	Kind            string
	Label           string
	APIOrigin       string
	Email           string
	Password        string
	SubscriptionURL string
}

func New(root string, cfg config.Config, st *store.Store) *Service {
	return &Service{
		Root:      root,
		Config:    cfg,
		Store:     st,
		Protector: secret.New(filepath.Join(root, ".run")),
		Fetcher:   subscription.NewFetcher(15 * time.Second),
	}
}

func (s *Service) Probe() Probe {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.probe
}

func (s *Service) PrimeFromCache() {
	active := s.Store.ActiveSource()
	s.PrimeSource(active)
}

func (s *Service) PrimeSource(sourceName string) bool {
	cached, err := s.Store.Cache(sourceName)
	if err != nil || cached == nil {
		return false
	}
	expected := s.sourceURL(sourceName)
	if expected != "" && cached.URL != "" && cached.URL != expected {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.probe = Probe{
		At:       cached.FetchedAt * 1000,
		Nodes:    CountNodes(cached.YAML),
		YAML:     cached.YAML,
		UserInfo: cached.UserInfo,
		Source:   sourceName,
	}
	return true
}

func (s *Service) sourceURL(sourceName string) string {
	switch {
	case sourceName == "local":
		raw, _ := s.LocalSubscriptionURL()
		return raw
	case sourceName == "manual":
		return strings.TrimSpace(s.Store.ManualURL())
	case strings.HasPrefix(sourceName, "account:"):
		account, _ := s.Store.Account(strings.TrimPrefix(sourceName, "account:"))
		if account != nil {
			return account.SubscriptionURL
		}
	}
	return ""
}

// DecodeURL powers the decrypt tool. It deliberately uses a loopback-capable
// fetcher: the URL comes from the person using the panel (usually the panel's own
// http:// LAN address), and the request originates on this machine. Refresh and
// account login keep using the strict fetcher, so this does not widen SSRF
// exposure for automatic/untrusted paths.
//
// It returns both variants: RawYAML (as the provider sent it) and YAML (after the
// Clash template). The tool shows both so the user can compare before trusting the
// rewritten config.
func (s *Service) DecodeURL(ctx context.Context, rawURL string) (LoginResult, error) {
	body, header, err := s.loopbackFetcher().FetchSubscription(ctx, rawURL)
	if err != nil {
		return LoginResult{}, err
	}
	raw, err := subscription.DecryptProfile(body)
	if err != nil {
		return LoginResult{}, err
	}
	yamlText, err := subscription.ApplyTemplate(raw)
	if err != nil {
		return LoginResult{}, err
	}
	return LoginResult{
		SubscriptionURL: rawURL,
		YAML:            yamlText,
		RawYAML:         raw,
		UserInfo:        subscription.ParseUserInfo(header),
	}, nil
}

var (
	loopbackOnce    sync.Once
	loopbackFetcher *subscription.Fetcher
)

func (s *Service) loopbackFetcher() *subscription.Fetcher {
	loopbackOnce.Do(func() {
		loopbackFetcher = subscription.NewLoopbackFetcher(15 * time.Second)
		if s.Transport != nil {
			loopbackFetcher.Client.Transport = s.Transport
		}
	})
	return loopbackFetcher
}

func (s *Service) ResetProbe() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.probe = Probe{}
}

func (s *Service) Refresh(ctx context.Context, force bool) error {
	s.mu.Lock()
	if s.refresh {
		s.mu.Unlock()
		return nil
	}
	if !force && s.probe.At > 0 {
		s.mu.Unlock()
		return nil
	}
	s.refresh = true
	active := s.Store.ActiveSource()
	s.mu.Unlock()

	var (
		yamlText string
		rawYAML  string
		userInfo *model.UserInfo
		finalURL string
		err      error
	)
	defer func() {
		s.mu.Lock()
		s.refresh = false
		s.mu.Unlock()
	}()

	resolved, resolveErr := s.Resolve(ctx, active)
	if resolveErr == nil {
		result, fetchErr := s.FetchResolved(ctx, resolved)
		if fetchErr == nil {
			yamlText, userInfo, finalURL = result.YAML, result.UserInfo, result.SubscriptionURL
			rawYAML = result.RawYAML
		} else {
			err = fetchErr
		}
	} else {
		err = resolveErr
	}

	now := time.Now().Unix()
	s.mu.Lock()
	defer s.mu.Unlock()
	if err != nil {
		s.probe.Error = err.Error()
		s.probe.Source = active
		s.probe.At = now * 1000
		if strings.HasPrefix(active, "account:") {
			_ = s.Store.UpdateAccountRuntime(strings.TrimPrefix(active, "account:"), "", 0, 0, nil, err.Error())
		}
		if s.probe.YAML == "" {
			if cached, _ := s.Store.Cache(active); cached != nil {
				s.probe.YAML = cached.YAML
				s.probe.Nodes = CountNodes(cached.YAML)
				s.probe.UserInfo = cached.UserInfo
				s.probe.Source = cached.Source
			}
		}
		return err
	}

	s.probe.YAML = yamlText
	s.probe.Nodes = CountNodes(yamlText)
	s.probe.UserInfo = userInfo
	s.probe.Error = ""
	s.probe.Source = active
	s.probe.At = now * 1000

	if strings.HasPrefix(active, "account:") {
		_ = s.Store.UpdateAccountRuntime(strings.TrimPrefix(active, "account:"), finalURL, 0, now, userInfo, "")
	}
	label := resolved.Label
	_ = s.Store.PutCache(model.Snapshot{
		Source:    active,
		URL:       finalURL,
		Label:     label,
		FetchedAt: now,
		YAML:      yamlText,
		RawYAML:   rawYAML,
		UserInfo:  userInfo,
	})
	return nil
}

func (s *Service) Resolve(ctx context.Context, sourceName string) (resolvedSource, error) {
	if sourceName == "" {
		sourceName = "local"
	}
	switch {
	case sourceName == "local":
		raw, err := s.LocalSubscriptionURL()
		if err != nil {
			return resolvedSource{}, err
		}
		return resolvedSource{Source: model.Source{Kind: "local", Label: "本机客户端", URL: raw}}, nil
	case sourceName == "manual":
		raw := strings.TrimSpace(s.Store.ManualURL())
		if raw == "" {
			return resolvedSource{}, errors.New("手动订阅地址为空")
		}
		return resolvedSource{Source: model.Source{Kind: "manual", Label: "手动链接", URL: raw}}, nil
	case strings.HasPrefix(sourceName, "account:"):
		id := strings.TrimPrefix(sourceName, "account:")
		account, err := s.Store.Account(id)
		if err != nil {
			return resolvedSource{}, err
		}
		if account == nil {
			return resolvedSource{}, errors.New("账号不存在")
		}
		label := account.Label
		if label == "" {
			label = account.Email
		}
		if label == "" {
			label = hostLabel(account.SubscriptionURL)
		}
		// A link account has no credentials: go straight to its URL. A password
		// account reuses a cached subscription_url when present, otherwise logs in.
		if account.Kind == model.KindLink {
			if account.SubscriptionURL == "" {
				return resolvedSource{}, errors.New("该账号没有订阅链接")
			}
			return resolvedSource{Source: model.Source{Kind: "account", ID: id, Label: label, URL: account.SubscriptionURL}}, nil
		}
		if account.SubscriptionURL != "" {
			return resolvedSource{Source: model.Source{Kind: "account", ID: id, Label: label, URL: account.SubscriptionURL}}, nil
		}
		login, err := s.LoginAccount(ctx, *account)
		if err != nil {
			return resolvedSource{}, err
		}
		return resolvedSource{
			Source: model.Source{Kind: "account", ID: id, Label: label, URL: login.SubscriptionURL},
			Fresh:  login,
		}, nil
	default:
		return resolvedSource{}, errors.New("未知的数据源")
	}
}

func (s *Service) FetchResolved(ctx context.Context, source resolvedSource) (LoginResult, error) {
	if source.Fresh != nil {
		return *source.Fresh, nil
	}
	result, err := s.fetchYAML(ctx, source.URL)
	if err == nil {
		return result, nil
	}
	if source.Kind != "account" || !subscription.IsStaleSubscription(err) {
		return LoginResult{}, err
	}

	account, getErr := s.Store.Account(source.ID)
	if getErr != nil || account == nil {
		return LoginResult{}, err
	}
	login, loginErr := s.LoginAccount(ctx, *account)
	if loginErr != nil {
		return LoginResult{}, fmt.Errorf("重新登录失败：%w", loginErr)
	}
	return *login, nil
}

func (s *Service) fetchYAML(ctx context.Context, rawURL string) (LoginResult, error) {
	body, header, err := s.Fetcher.FetchSubscription(ctx, rawURL)
	if err != nil {
		return LoginResult{}, err
	}
	raw, err := subscription.DecryptProfile(body)
	if err != nil {
		return LoginResult{}, err
	}
	yamlText, err := subscription.ApplyTemplate(raw)
	if err != nil {
		return LoginResult{}, err
	}
	return LoginResult{SubscriptionURL: rawURL, YAML: yamlText, RawYAML: raw, UserInfo: subscription.ParseUserInfo(header)}, nil
}

// apiEnvelope is the panel's response wrapper. Real deployments disagree on the
// `status` type (bool, 0/1, or "1"/"true"), so it is decoded permissively instead
// of as a plain bool; a strict bool breaks every endpoint on such panels.
type apiEnvelope struct {
	Status  flexibleBool    `json:"status"`
	Data    json.RawMessage `json:"data"`
	Message string          `json:"message"`
}

// flexibleBool accepts bool, number and string forms. Present reports whether the
// field appeared at all, so callers can tell "absent" from "false".
type flexibleBool struct {
	value   bool
	present bool
}

func (b *flexibleBool) UnmarshalJSON(raw []byte) error {
	b.present = true
	text := strings.Trim(strings.TrimSpace(string(raw)), `"`)
	switch strings.ToLower(text) {
	case "", "null":
		b.present = false
		return nil
	case "1", "true", "yes", "ok", "success":
		b.value = true
		return nil
	case "0", "false", "no", "error", "fail":
		b.value = false
		return nil
	}
	// Unknown literal: keep the raw payload out of the error so a WAF HTML page
	// or an unexpected schema surfaces as a readable message, not a Go type error.
	return fmt.Errorf("status 字段格式未知: %.40s", text)
}

// looksLikeHTML reports whether a response body is an HTML page (WAF, captcha,
// error portal) rather than the JSON the API contract promises.
func looksLikeHTML(body []byte) bool {
	trimmed := strings.TrimSpace(string(body))
	return strings.HasPrefix(trimmed, "<")
}

func (s *Service) LoginAccount(ctx context.Context, account model.Account) (*LoginResult, error) {
	password, err := s.Protector.Decrypt(account.PasswordEnc)
	if err != nil {
		return nil, fmt.Errorf("密码解密失败: %w", err)
	}
	if password == "" {
		return nil, errors.New("该账号缺少邮箱或密码，请重新填写")
	}

	origin := strings.TrimSpace(account.APIOrigin)
	if origin == "" {
		origin = defaultAPIOrigin
	}
	base, err := url.Parse(origin)
	if err != nil || base.Scheme != "https" || base.Host == "" {
		return nil, errors.New("API 域名必须使用 HTTPS")
	}
	base.Path = ""
	base.RawQuery = ""
	base.Fragment = ""

	jar, _ := cookiejar.New(nil)
	apiClient := &http.Client{
		Timeout:   15 * time.Second,
		Jar:       jar,
		Transport: s.Transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	subClient := &http.Client{Timeout: 15 * time.Second, Jar: jar, Transport: s.Transport}
	authorization := ""

	apiJSON := func(path string, form url.Values) (json.RawMessage, error) {
		endpoint := base.String() + "/api/v1" + path
		method := http.MethodGet
		var body io.Reader
		if form != nil {
			method = http.MethodPost
			body = strings.NewReader(form.Encode())
		}
		req, reqErr := http.NewRequestWithContext(ctx, method, endpoint, body)
		if reqErr != nil {
			return nil, reqErr
		}
		req.Header.Set("User-Agent", subscription.APIUA)
		req.Header.Set("Accept", "application/json")
		if authorization != "" {
			req.Header.Set("Authorization", authorization)
		}
		if form != nil {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
		resp, reqErr := apiClient.Do(req)
		if reqErr != nil {
			return nil, reqErr
		}
		defer resp.Body.Close()
		data, readErr := subscription.ReadCapped(resp)
		if readErr != nil {
			return nil, readErr
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return nil, subscription.ErrorFromResponse(resp, data)
		}
		var env apiEnvelope
		if err := json.Unmarshal(data, &env); err != nil {
			// A WAF/captcha page is HTML, not JSON; say so instead of leaking a
			// Go decoder error that reads like a client bug.
			if looksLikeHTML(data) {
				return nil, errors.New("接口返回了网页而不是 JSON，可能被 WAF / 人机验证拦截；请换网络或稍后重试")
			}
			return nil, fmt.Errorf("接口返回非 JSON: %w", err)
		}
		if env.Status.present && !env.Status.value {
			if env.Message != "" {
				return nil, fmt.Errorf("接口返回失败 / %s", env.Message)
			}
			return nil, errors.New("接口返回失败")
		}
		return env.Data, nil
	}

	loginData, err := apiJSON("/passport/auth/login", url.Values{
		"email":    {account.Email},
		"password": {password},
	})
	if err != nil {
		return nil, err
	}
	var login struct {
		AuthData string `json:"auth_data"`
	}
	if err := json.Unmarshal(loginData, &login); err != nil || login.AuthData == "" {
		return nil, errors.New("没有返回 data.auth_data，需核对该 API 的登录协议")
	}
	authorization = login.AuthData

	if _, err := apiJSON("/user/info", nil); err != nil {
		return nil, err
	}
	subscribeData, err := apiJSON("/user/getSubscribe", nil)
	if err != nil {
		return nil, err
	}
	var subscribe struct {
		URL string `json:"subscribe_url"`
	}
	if err := json.Unmarshal(subscribeData, &subscribe); err != nil || subscribe.URL == "" {
		return nil, errors.New("没有返回 data.subscribe_url")
	}

	subscribeURL, err := url.Parse(subscribe.URL)
	if err != nil {
		return nil, err
	}
	subscribeURL = base.ResolveReference(subscribeURL)
	if subscribeURL.Hostname() == "api.example.com" {
		subscribeURL.Scheme = base.Scheme
		subscribeURL.Host = base.Host
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, subscribeURL.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", subscription.SubscriptionUA)
	if subscribeURL.Scheme == base.Scheme && subscribeURL.Host == base.Host {
		req.Header.Set("Authorization", authorization)
	}
	resp, err := subClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := subscription.ReadCapped(resp)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, subscription.ErrorFromResponse(resp, body)
	}
	raw, err := subscription.DecryptProfile(body)
	if err != nil {
		return nil, err
	}
	yamlText, err := subscription.ApplyTemplate(raw)
	if err != nil {
		return nil, err
	}
	userInfo := subscription.ParseUserInfo(resp.Header.Get("Subscription-Userinfo"))
	result := &LoginResult{
		SubscriptionURL: subscribeURL.String(),
		YAML:            yamlText,
		RawYAML:         raw,
		UserInfo:        userInfo,
	}
	_ = s.Store.UpdateAccountRuntime(account.ID, result.SubscriptionURL, time.Now().Unix(), 0, userInfo, "")
	return result, nil
}

// ------------------------------------------------------------- local source

func (s *Service) LocalSubscriptionURL() (string, error) {
	if raw := strings.TrimSpace(os.Getenv("FASTLINK_SUB_URL")); raw != "" {
		return raw, nil
	}
	appData := os.Getenv("APPDATA")
	if appData == "" {
		return "", errors.New("未找到 APPDATA")
	}
	dir := filepath.Join(appData, "FastLink", "FastLink")

	currentID := int64(0)
	if raw, err := os.ReadFile(filepath.Join(dir, "shared_preferences.json")); err == nil {
		var prefs map[string]json.RawMessage
		if json.Unmarshal(raw, &prefs) == nil {
			var flutter string
			if json.Unmarshal(prefs["flutter.config"], &flutter) == nil {
				var fc struct {
					CurrentProfileID int64 `json:"currentProfileId"`
				}
				_ = json.Unmarshal([]byte(flutter), &fc)
				currentID = fc.CurrentProfileID
			}
		}
	}

	dbPath := filepath.Join(dir, "database.sqlite")
	db, err := sql.Open("sqlite", "file:"+dbPath+"?mode=ro")
	if err != nil {
		return "", err
	}
	defer db.Close()

	var rawURL string
	if currentID > 0 {
		_ = db.QueryRow(`SELECT url FROM profiles WHERE id = ?`, currentID).Scan(&rawURL)
	}
	if rawURL == "" {
		_ = db.QueryRow(`SELECT url FROM profiles ORDER BY id LIMIT 1`).Scan(&rawURL)
	}
	if rawURL == "" {
		return "", errors.New("未找到客户端订阅")
	}
	return rawURL, nil
}

// ----------------------------------------------------------------- accounts

func (s *Service) PublicAccounts() (map[string]any, error) {
	active := s.Store.ActiveSource()
	accounts, err := s.Store.Accounts()
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"active":    active,
		"manualUrl": s.Store.ManualURL(),
		"accounts":  accounts,
	}, nil
}

func (s *Service) UpsertAccount(input AccountInput) (*model.Account, error) {
	kind := strings.TrimSpace(input.Kind)
	if kind == "" {
		kind = model.KindPassword
	}
	if kind != model.KindPassword && kind != model.KindLink {
		return nil, errors.New("未知的账号类型")
	}
	email := strings.TrimSpace(input.Email)
	apiOrigin := strings.TrimSpace(input.APIOrigin)
	subURL := strings.TrimSpace(input.SubscriptionURL)

	// Per-kind required fields. Editing a password account may omit the password
	// (blank means "keep the current one"), but a brand-new one must supply it.
	if kind == model.KindPassword {
		if email == "" {
			return nil, errors.New("请填写邮箱")
		}
	} else if subURL == "" {
		return nil, errors.New("请填写订阅链接")
	}

	accounts, err := s.Store.Accounts()
	if err != nil {
		return nil, err
	}
	var existing *model.Account
	if input.ID != "" {
		existing, _ = s.Store.Account(input.ID)
	}
	// Only match an existing row for password accounts: two link accounts may
	// legitimately point at the same URL (e.g. re-importing), and matching on an
	// empty email would otherwise collapse them into one.
	if existing == nil && kind == model.KindPassword && email != "" {
		for i := range accounts {
			if accounts[i].Kind == model.KindPassword && accounts[i].Email == email && accounts[i].APIOrigin == apiOrigin {
				existing = &accounts[i]
				break
			}
		}
	}
	if existing != nil && existing.Kind != kind {
		return nil, errors.New("账号类型不可修改，请新建账号")
	}
	// Names must be unique across both kinds: the list is the only place a user
	// tells accounts apart, and the subscription chips reuse the same label.
	// Comparison is case-insensitive and trimmed, so "A" and "a " collide.
	if label := strings.TrimSpace(input.Label); label != "" {
		for i := range accounts {
			if existing != nil && accounts[i].ID == existing.ID {
				continue
			}
			if strings.EqualFold(strings.TrimSpace(accounts[i].Label), label) {
				return nil, fmt.Errorf("账号名称「%s」已存在，请换一个", label)
			}
		}
	}
	id := input.ID
	if id == "" && existing != nil {
		id = existing.ID
	}
	if id == "" {
		buf := make([]byte, 6)
		_, _ = rand.Read(buf)
		id = "acc_" + hex.EncodeToString(buf)
	}
	if kind == model.KindPassword && existing == nil && input.Password == "" {
		return nil, errors.New("新账号必须填写密码")
	}

	passwordEnc := ""
	lastLogin, lastFetch := int64(0), int64(0)
	var userInfo *model.UserInfo
	subscriptionURL := ""
	errorText := ""
	passwordChanged := false
	if existing != nil {
		passwordEnc = existing.PasswordEnc
		lastLogin, lastFetch = existing.LastLogin, existing.LastFetch
		userInfo = existing.UserInfo
		subscriptionURL = existing.SubscriptionURL
	}
	if kind == model.KindPassword && input.Password != "" {
		passwordEnc, err = s.Protector.Encrypt(input.Password)
		if err != nil {
			return nil, err
		}
		passwordChanged = existing != nil && input.Password != ""
	}
	if kind == model.KindLink {
		// A link account keeps the URL the user typed; clearing credentials makes
		// the stored shape unambiguous.
		passwordEnc = ""
		subscriptionURL = subURL
	}

	label := strings.TrimSpace(input.Label)
	if label == "" {
		if kind == model.KindPassword {
			label = email
		} else {
			label = hostLabel(subURL)
		}
	}
	account := model.Account{
		Kind:            kind,
		ID:              id,
		Label:           label,
		APIOrigin:       apiOrigin,
		Email:           email,
		PasswordEnc:     passwordEnc,
		HasPassword:     passwordEnc != "",
		SubscriptionURL: subscriptionURL,
		LastLogin:       lastLogin,
		LastFetch:       lastFetch,
		UserInfo:        userInfo,
		Error:           errorText,
	}
	if err := s.Store.UpsertAccount(account); err != nil {
		return nil, err
	}
	if passwordChanged {
		// The old token was minted with the previous password; force a re-login.
		if err := s.Store.ClearAccountRuntime(id); err != nil {
			return nil, err
		}
		account.SubscriptionURL = ""
		account.LastLogin, account.LastFetch = 0, 0
		account.UserInfo = nil
	}
	_, _ = s.Store.EnsureSubKey("account:" + id)
	return &account, nil
}

// hostLabel derives a short display label from a subscription URL.
func hostLabel(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return raw
	}
	return u.Host
}

// ActivateAccount makes the given account the active source. Kept separate from
// UpsertAccount so the UI can switch accounts without touching their fields.
func (s *Service) ActivateAccount(id string) error {
	account, err := s.Store.Account(id)
	if err != nil {
		return err
	}
	if account == nil {
		return errors.New("账号不存在")
	}
	return s.SetSource("account:"+id, "")
}

func (s *Service) DeleteAccount(id string) error {
	if err := s.Store.DeleteAccount(id); err != nil {
		return err
	}
	return nil
}

func (s *Service) SetSource(sourceName, manualURL string) error {
	sourceName = strings.TrimSpace(sourceName)
	switch {
	case sourceName == "local" || sourceName == "manual":
	case strings.HasPrefix(sourceName, "account:"):
		account, err := s.Store.Account(strings.TrimPrefix(sourceName, "account:"))
		if err != nil || account == nil {
			return errors.New("账号不存在")
		}
	default:
		return errors.New("未知的数据源")
	}
	if sourceName == "manual" {
		if err := s.Store.SetManualURL(strings.TrimSpace(manualURL)); err != nil {
			return err
		}
	}
	return s.Store.SetActiveSource(sourceName)
}

// ------------------------------------------------------------------- state

func (s *Service) State() StateData {
	s.mu.Lock()
	probe := s.probe
	s.mu.Unlock()
	active := s.Store.ActiveSource()
	cached, _ := s.Store.Cache(active)
	label := ""
	if cached != nil {
		label = cached.Label
	}
	if label == "" {
		switch {
		case active == "local":
			label = "本机客户端"
		case active == "manual":
			label = "手动链接"
		case strings.HasPrefix(active, "account:"):
			if account, _ := s.Store.Account(strings.TrimPrefix(active, "account:")); account != nil {
				label = account.Label
				if label == "" {
					label = account.Email
				}
			}
		}
	}
	addresses := s.Addresses(active)
	subURL := ""
	if len(addresses) > 0 {
		subURL = addresses[0].URL
	}
	nodeList := []model.Node{}
	if probe.YAML != "" {
		nodeList = ExtractNodes(probe.YAML)
	}
	return StateData{
		Healthy:    probe.At > 0 && (probe.Nodes > 0 || probe.Error == ""),
		Nodes:      probe.Nodes,
		UserInfo:   probe.UserInfo,
		ProbeError: probe.Error,
		Pending:    probe.At == 0,
		SubURL:     subURL,
		NodeList:   nodeList,
		Addresses:  addresses,
		Active:     active,
		Label:      label,
		FetchedAt:  probe.At / 1000,
		Cached:     cached != nil,
	}
}

func (s *Service) Addresses(sourceName string) []model.Address {
	key, _ := s.Store.EnsureSubKey(sourceName)
	if key == "" {
		key = s.Config.AccessKey
	}
	out := []model.Address{}
	seen := map[string]bool{}
	add := func(label, raw string) {
		raw = strings.TrimSpace(raw)
		if raw == "" || seen[raw] {
			return
		}
		seen[raw] = true
		out = append(out, model.Address{Label: label, URL: raw, RawURL: withVariant(raw, "raw")})
	}
	withKey := func(raw string) string {
		u, err := url.Parse(raw)
		if err != nil {
			return raw
		}
		q := u.Query()
		if key != "" {
			q.Set("key", key)
		}
		u.RawQuery = q.Encode()
		return u.String()
	}

	lan := config.LANAddresses()
	if s.Config.Host == "0.0.0.0" && len(lan) > 0 {
		add(lan[0], withKey(fmt.Sprintf("http://%s:%d/sub", lan[0], s.Config.Port)))
		add("本机", withKey(fmt.Sprintf("http://127.0.0.1:%d/sub", s.Config.Port)))
		for _, ip := range lan[1:] {
			add(ip, withKey(fmt.Sprintf("http://%s:%d/sub", ip, s.Config.Port)))
		}
	} else {
		add("本机", withKey(fmt.Sprintf("http://127.0.0.1:%d/sub", s.Config.Port)))
	}

	for _, raw := range s.Config.Domains {
		value := strings.TrimSpace(raw)
		if value == "" {
			continue
		}
		if strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://") {
			u, err := url.Parse(value)
			if err != nil {
				continue
			}
			if !strings.HasSuffix(strings.TrimRight(u.Path, "/"), "/sub") {
				u.Path = strings.TrimRight(u.Path, "/") + "/sub"
			}
			add(u.Host, withKey(u.String()))
		} else {
			add(value, withKey(fmt.Sprintf("http://%s:%d/sub", value, s.Config.Port)))
		}
	}
	return out
}

// withVariant returns raw with variant=<v> set, keeping every other query param.
func withVariant(raw, v string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	q := u.Query()
	q.Set("variant", v)
	u.RawQuery = q.Encode()
	return u.String()
}

func (s *Service) RotateSubKey(sourceName string) (string, error) {
	return s.Store.RotateSubKey(sourceName)
}

// ------------------------------------------------------------------- nodes

var (
	flowNode  = regexp.MustCompile(`^\s*-\s*\{(.*)\}\s*$`)
	flowName  = regexp.MustCompile(`"name"\s*:\s*"([^"]*)"`)
	flowSrv   = regexp.MustCompile(`"server"\s*:\s*"([^"]*)"`)
	blockNode = regexp.MustCompile(`(?m)^\s*-\s*name:\s*"?([^"\n]+)"?\s*$(?:\n\s+server:\s*"?([^"\n]+)"?)?`)
)

func ExtractNodes(yamlText string) []model.Node {
	cut := strings.Index(yamlText, "proxy-groups:")
	head := yamlText
	if cut > 0 {
		head = yamlText[:cut]
	}
	out := []model.Node{}
	for _, line := range strings.Split(head, "\n") {
		if m := flowNode.FindStringSubmatch(line); m != nil {
			name := flowName.FindStringSubmatch(m[1])
			if name == nil {
				continue
			}
			server := ""
			if s := flowSrv.FindStringSubmatch(m[1]); s != nil {
				server = s[1]
			}
			out = append(out, model.Node{Name: name[1], Server: server})
		}
	}
	if len(out) > 0 {
		return out
	}
	for _, m := range blockNode.FindAllStringSubmatch(head, -1) {
		out = append(out, model.Node{Name: strings.TrimSpace(m[1]), Server: strings.TrimSpace(m[2])})
	}
	return out
}

func CountNodes(yamlText string) int {
	cut := strings.Index(yamlText, "proxy-groups:")
	head := yamlText
	if cut > 0 {
		head = yamlText[:cut]
	}
	count := 0
	for _, line := range strings.Split(head, "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "- {") || strings.HasPrefix(t, "- name:") || strings.HasPrefix(t, "- \"name\":") {
			count++
		}
	}
	return count
}
