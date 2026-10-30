package httpapi

import (
	"context"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"fastlink-tool/internal/config"
	"fastlink-tool/internal/model"
	"fastlink-tool/internal/platform"
	"fastlink-tool/internal/service"
	"fastlink-tool/internal/subscription"
)

type Server struct {
	Service *service.Service
	Config  config.Config
	Root    string
	Web     fs.FS
	Version string
	Quit    func()
}

func New(svc *service.Service, cfg config.Config, root string, web fs.FS, version string, quit func()) *Server {
	return &Server{Service: svc, Config: cfg, Root: root, Web: web, Version: version, Quit: quit}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.health)
	mux.HandleFunc("POST /api/v1/login", s.login)
	mux.HandleFunc("POST /api/v1/logout", s.logout)
	mux.HandleFunc("GET /api/v1/state", s.auth(s.state))
	mux.HandleFunc("GET /api/v1/accounts", s.auth(s.accountsGet))
	mux.HandleFunc("POST /api/v1/accounts", s.auth(s.accountsPost))
	mux.HandleFunc("POST /api/v1/source", s.auth(s.source))
	mux.HandleFunc("POST /api/v1/refresh", s.auth(s.refresh))
	mux.HandleFunc("POST /api/v1/sub-key", s.auth(s.subKey))
	mux.HandleFunc("POST /api/v1/decrypt", s.auth(s.decrypt))
	mux.HandleFunc("POST /api/v1/account", s.auth(s.tempAccount))
	mux.HandleFunc("POST /api/v1/account/password", s.auth(s.accountPassword))
	mux.HandleFunc("GET /api/v1/config", s.auth(s.configGet))
	mux.HandleFunc("GET /api/v1/net-diag", s.auth(s.netDiag))
	mux.HandleFunc("POST /api/v1/config", s.auth(s.configPost))
	mux.HandleFunc("GET /api/v1/logs", s.auth(s.logs))
	mux.HandleFunc("POST /api/v1/quit", s.auth(s.quit))
	mux.HandleFunc("/sub", s.sub)
	mux.HandleFunc("/clash", s.sub)
	mux.HandleFunc("/config.yaml", s.sub)
	mux.Handle("/", http.FileServer(http.FS(s.Web)))
	return mux
}

// --------------------------------------------------------------------- auth

func suppliedKey(r *http.Request) string {
	if header := r.Header.Get("X-Access-Key"); header != "" {
		return header
	}
	return r.URL.Query().Get("key")
}

func equal(a, b string) bool {
	return len(a) == len(b) && subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

func (s *Server) authed(r *http.Request) bool {
	if s.Config.AccessKey == "" {
		return true
	}
	if key := suppliedKey(r); key != "" && equal(key, s.Config.AccessKey) {
		return true
	}
	if cookie, err := r.Cookie("fl_key"); err == nil && equal(cookie.Value, s.Config.AccessKey) {
		return true
	}
	return false
}

func (s *Server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.authed(r) {
			writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "需要口令", "login": true})
			return
		}
		next(w, r)
	}
}

// sourceKey returns the source named by a per-source subscription key.
func (s *Server) sourceKey(r *http.Request) string {
	tokens := []string{r.URL.Query().Get("key"), r.Header.Get("X-Access-Key")}
	subKeys, err := s.Service.Store.SubKeys()
	if err != nil {
		return ""
	}
	for _, token := range tokens {
		if token == "" {
			continue
		}
		for source, key := range subKeys {
			if key != "" && equal(key, token) {
				return source
			}
		}
	}
	return ""
}

// ------------------------------------------------------------------- helpers

func writeJSON(w http.ResponseWriter, status int, value any) {
	body, err := json.Marshal(value)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

func readJSON(r *http.Request, dst any) error {
	decoder := json.NewDecoder(io.LimitReader(r.Body, 128*1024))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("ok\n"))
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Key      string `json:"key"`
		Remember bool   `json:"remember"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	if !equal(body.Key, s.Config.AccessKey) {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "口令不正确"})
		return
	}
	maxAge := 12 * 3600
	if body.Remember {
		maxAge = 30 * 24 * 3600
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "fl_key",
		Value:    s.Config.AccessKey,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   maxAge,
	})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) logout(w http.ResponseWriter, _ *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: "fl_key", Value: "", Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: -1})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// -------------------------------------------------------------------- state

func (s *Server) state(w http.ResponseWriter, r *http.Request) {
	go func() { _ = s.Service.Refresh(context.Background(), false) }()
	state := s.Service.State()
	activeKey, _ := s.Service.Store.SubKey(state.Active)
	source := map[string]any{
		"active":    state.Active,
		"resolved":  state.Active,
		"label":     state.Label,
		"key":       activeKey,
		"fetchedAt": state.FetchedAt,
		"cached":    state.Cached,
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"pid":        os.Getpid(),
		"healthy":    state.Healthy,
		"nodes":      state.Nodes,
		"userInfo":   state.UserInfo,
		"probeError": state.ProbeError,
		"pending":    state.Pending,
		"subUrl":     state.SubURL,
		"nodeList":   state.NodeList,
		"addresses":  state.Addresses,
		"config": map[string]any{
			"host":    s.Config.Host,
			"port":    s.Config.Port,
			"domains": s.Config.Domains,
		},
		"lan":    config.LANAddresses(),
		"source": source,
	})
}

func (s *Server) accountsGet(w http.ResponseWriter, _ *http.Request) {
	value, err := s.Service.PublicAccounts()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, value)
}

func (s *Server) accountsPost(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	if action, _ := body["action"].(string); action == "delete" {
		id, _ := body["id"].(string)
		before := s.Service.Store.ActiveSource()
		if err := s.Service.DeleteAccount(id); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		after := s.Service.Store.ActiveSource()
		if before != after {
			if !s.Service.PrimeSource(after) {
				_ = s.Service.Refresh(context.Background(), true)
			}
		}
		value, _ := s.Service.PublicAccounts()
		writeJSON(w, http.StatusOK, value)
		return
	}
	if action, _ := body["action"].(string); action == "activate" {
		id, _ := body["id"].(string)
		if err := s.Service.ActivateAccount(id); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		// Prefer the cached snapshot so switching accounts is instant; fall back to
		// a real fetch when this account has nothing cached yet.
		if !s.Service.PrimeSource("account:" + id) {
			s.Service.ResetProbe()
			_ = s.Service.Refresh(context.Background(), true)
		}
		value, _ := s.Service.PublicAccounts()
		writeJSON(w, http.StatusOK, value)
		return
	}
	text := func(key string) string { v, _ := body[key].(string); return v }
	_, err := s.Service.UpsertAccount(service.AccountInput{
		ID:              text("id"),
		Kind:            text("kind"),
		Label:           text("label"),
		Email:           text("email"),
		Password:        text("password"),
		SubscriptionURL: text("subscriptionUrl"),
		SubKey:          text("subKey"),
	})
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	value, _ := s.Service.PublicAccounts()
	writeJSON(w, http.StatusOK, value)
}

func (s *Server) source(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Source      string `json:"source"`
		ManualURL   string `json:"manualUrl"`
		ManualLabel string `json:"manualLabel"`
		Refresh     bool   `json:"refresh"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	before := s.Service.Store.ActiveSource()
	if body.Source == "" {
		body.Source = before
	}
	manualURL := body.ManualURL
	if body.Source == "manual" && manualURL == "" {
		manualURL = s.Service.Store.ManualURL()
	}
	if body.Source == "manual" {
		if err := s.Service.Store.SetManualLabel(strings.TrimSpace(body.ManualLabel)); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
	}
	unchanged := body.Source == before && (body.Source != "manual" || manualURL == s.Service.Store.ManualURL())
	if unchanged && !body.Refresh && s.Service.Probe().At > 0 {
		writeJSON(w, http.StatusOK, s.sourceResponse(true))
		return
	}
	if err := s.Service.SetSource(body.Source, manualURL); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	cached := false
	if !body.Refresh && s.Service.PrimeSource(body.Source) {
		cached = true
	} else {
		s.Service.ResetProbe()
		_ = s.Service.Refresh(context.Background(), true)
	}
	writeJSON(w, http.StatusOK, s.sourceResponse(cached))
}

func (s *Server) sourceResponse(cached bool) map[string]any {
	value, _ := s.Service.PublicAccounts()
	probe := s.Service.Probe()
	return map[string]any{
		"active":     value["active"],
		"manualUrl":  value["manualUrl"],
		"accounts":   value["accounts"],
		"nodes":      probe.Nodes,
		"probeError": probe.Error,
		"cached":     cached,
	}
}

func (s *Server) refresh(w http.ResponseWriter, _ *http.Request) {
	err := s.Service.Refresh(context.Background(), true)
	probe := s.Service.Probe()
	writeJSON(w, http.StatusOK, map[string]any{
		"nodes":      probe.Nodes,
		"userInfo":   probe.UserInfo,
		"probeError": firstError(err, probe.Error),
		"fetchedAt":  probe.At / 1000,
	})
}

func firstError(err error, fallback string) string {
	if err != nil {
		return err.Error()
	}
	return fallback
}

func (s *Server) subKey(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Source string `json:"source"`
		Key    string `json:"key"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	source := body.Source
	if source == "" {
		source = s.Service.Store.ActiveSource()
	}
	var (
		key string
		err error
	)
	if strings.TrimSpace(body.Key) == "" {
		// An empty key is the only way to get a generated one: rotation is an
		// explicit user action, never something refresh or save does on its own.
		key, err = s.Service.RotateSubKey(source)
	} else {
		key, err = s.Service.SetSubKey(source, body.Key)
	}
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"source":    source,
		"key":       key,
		"addresses": s.Service.Addresses(source),
	})
}

func (s *Server) decrypt(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Input string `json:"input"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	target, err := extractTarget(body.Input)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	result, err := s.Service.DecodeURL(context.Background(), target)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"subscriptionUrl": target,
		"convertedUrl":    "/sub?b64=" + url.QueryEscape(base64.URLEncoding.EncodeToString([]byte(target))),
		"yaml":            result.YAML,
		// rawYaml is the provider's own output before the Clash template, so the
		// tool can offer both variants side by side.
		"rawYaml":  result.RawYAML,
		"userInfo": result.UserInfo,
		// apex is present only when the profile came from an APXS v2 container,
		// so the tool can say which key generation the payload used.
		"apex": apexJSON(result.Apex),
		// Node count comes from the same parser the subscription view uses, so the
		// tool agrees with the rest of the UI. A regex over the YAML cannot: this
		// template emits both block ("- name:") and flow ('- {"name":...}') forms.
		"nodes":    len(service.ExtractNodes(result.YAML)),
		"rawNodes": len(service.ExtractNodes(result.RawYAML)),
	})
}

// apexJSON flattens an APXS header for the browser. Returns nil for the older
// schemes so the field is omitted rather than rendered as a zeroed object.
func apexJSON(header *subscription.ApexHeader) map[string]any {
	if header == nil {
		return nil
	}
	return map[string]any{
		"version":    header.Version,
		"keyId":      header.KeyID,
		"encVersion": header.EncVersion,
		"trailerLen": header.TrailerLen,
	}
}

func (s *Server) tempAccount(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	account := model.Account{Label: body.Email, Email: body.Email, PasswordEnc: "plain:" + body.Password, HasPassword: body.Password != ""}
	result, err := s.Service.LoginAccount(context.Background(), account)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"subscriptionUrl": result.SubscriptionURL,
		"convertedUrl":    "/sub?b64=" + url.QueryEscape(base64.URLEncoding.EncodeToString([]byte(result.SubscriptionURL))),
		"yaml":            result.YAML,
		"userInfo":        result.UserInfo,
	})
}

func (s *Server) accountPassword(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID string `json:"id"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	password, err := s.Service.AccountPassword(body.ID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"password": password})
}

func (s *Server) configGet(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.Config)
}

// netDiag reports whether LAN devices are likely able to reach the panel. Read-only:
// it never modifies firewall state, because doing so would require elevation and the
// program should not silently widen the host's attack surface.
func (s *Server) netDiag(w http.ResponseWriter, _ *http.Request) {
	listening := s.Config.Host == "0.0.0.0"
	lan := config.LANAddresses()

	// Firewall probing shells out to PowerShell (~2s). Run it with a deadline so a
	// hung or missing PowerShell cannot stall the settings page; the UI shows an
	// "unknown" state instead of hanging forever.
	type result struct{ info platform.FirewallInfo }
	ch := make(chan result, 1)
	go func() { ch <- result{platform.InspectFirewall(s.Config.Port)} }()

	var fw platform.FirewallInfo
	select {
	case r := <-ch:
		fw = r.info
	case <-time.After(6 * time.Second):
		fw = platform.FirewallInfo{Supported: false, Detail: "防火墙检测超时。"}
	}

	reachable := listening && (len(fw.ProfilesEnabled) == 0 || fw.RuleFound || !fw.Supported)

	writeJSON(w, http.StatusOK, map[string]any{
		"listenHost": s.Config.Host,
		"port":       s.Config.Port,
		"lanOnly":    !listening,
		"lan":        lan,
		"firewall":   fw,
		"reachable":  reachable,
		"hint":       reachabilityHint(listening, fw, lan),
	})
}

func reachabilityHint(listening bool, fw platform.FirewallInfo, lan []string) string {
	if !listening {
		return "当前仅监听本机，局域网设备无法访问。需要时把「监听范围」改为「局域网 + 本机」。"
	}
	if !fw.Supported {
		return "已监听所有网卡；请自行确认系统防火墙放行了该端口。"
	}
	if len(fw.ProfilesEnabled) == 0 {
		return "已监听所有网卡，防火墙已关闭，局域网可直接访问。"
	}
	if fw.RuleFound {
		return "已监听所有网卡，且防火墙存在面板入站规则。"
	}
	return "已监听所有网卡，但防火墙没有面板入站规则，局域网设备可能被拦截。以管理员身份运行 scripts\\firewall.ps1 -Apply 创建规则。"
}

func (s *Server) configPost(w http.ResponseWriter, r *http.Request) {
	var patch struct {
		Host      string   `json:"host"`
		Port      int      `json:"port"`
		AccessKey *string  `json:"accessKey"`
		Domains   []string `json:"domains"`
		ProxyMode *string  `json:"proxyMode"`
	}
	if err := readJSON(r, &patch); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	next := s.Config
	oldHost, oldPort := next.Host, next.Port
	oldProxy := next.ProxyMode
	if patch.Host == "0.0.0.0" || patch.Host == "127.0.0.1" {
		next.Host = patch.Host
	}
	if patch.Port >= 1024 && patch.Port <= 65535 {
		next.Port = patch.Port
	}
	if patch.AccessKey != nil {
		next.AccessKey = strings.TrimSpace(*patch.AccessKey)
	}
	if patch.Domains != nil {
		next.Domains = patch.Domains
	}
	if patch.ProxyMode != nil {
		if *patch.ProxyMode == config.ProxySystem {
			next.ProxyMode = config.ProxySystem
		} else {
			next.ProxyMode = config.ProxyDirect
		}
	}
	if err := config.Save(s.Root, next); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	s.Config = next
	s.Service.Config = next
	if next.ProxyMode != oldProxy {
		// Drop pooled connections so the new proxy mode applies immediately
		// instead of waiting for the idle timeout.
		s.Service.ResetTransports()
	}
	writeJSON(w, http.StatusOK, map[string]any{"config": next, "restartRequired": next.Port != oldPort || next.Host != oldHost})
}

func (s *Server) logs(w http.ResponseWriter, r *http.Request) {
	lines := 120
	if n, err := strconv.Atoi(r.URL.Query().Get("lines")); err == nil && n > 0 && n <= 500 {
		lines = n
	}
	path := filepath.Join(s.Root, ".run", "server.out.log")
	raw, err := os.ReadFile(path)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"text": ""})
		return
	}
	all := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	if len(all) > lines {
		all = all[len(all)-lines:]
	}
	writeJSON(w, http.StatusOK, map[string]any{"text": strings.Join(all, "\n")})
}

func (s *Server) quit(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	if s.Quit != nil {
		go func() {
			time.Sleep(100 * time.Millisecond)
			s.Quit()
		}()
	}
}

// ---------------------------------------------------------------- subscription

func (s *Server) sub(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	sourceName := s.sourceKey(r)
	explicit := r.URL.Query().Has("b64") || r.URL.Query().Has("url")
	if sourceName == "" {
		if !s.authed(r) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
	}
	if sourceName != "" && explicit {
		http.Error(w, "source key cannot fetch an arbitrary URL", http.StatusBadRequest)
		return
	}

	var (
		result       service.LoginResult
		err          error
		explicitHost string
	)
	if explicit {
		target, targetErr := extractTargetRequest(r)
		if targetErr != nil {
			http.Error(w, targetErr.Error(), http.StatusBadRequest)
			return
		}
		explicitHost = hostOnly(target)
		result, err = s.Service.DecodeURL(r.Context(), target)
	} else {
		if sourceName == "" {
			sourceName = s.Service.Store.ActiveSource()
		}
		resolved, resolveErr := s.Service.Resolve(r.Context(), sourceName)
		if resolveErr != nil {
			err = resolveErr
		} else {
			result, err = s.Service.FetchResolved(r.Context(), resolved)
		}
	}
	if err != nil {
		http.Error(w, "decrypt failed: "+err.Error(), http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "text/yaml; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Profile-Update-Interval", "12")
	// Clients name a profile from Content-Disposition's filename; without it
	// they fall back to the last path segment ("sub"). The value mirrors what
	// real panels ship: "<host>/<label>".
	label, host := "", ""
	if explicit {
		host = explicitHost
	} else {
		label, host = s.Service.SourceMeta(sourceName)
	}
	if override := strings.TrimSpace(r.URL.Query().Get("name")); override != "" {
		label = override
	}
	w.Header().Set("Content-Disposition", profileFilename(label, host))
	if r.Host != "" {
		w.Header().Set("Profile-Web-Page-Url", "http://"+r.Host+"/")
	}
	if header := formatUserInfo(result.UserInfo); header != "" {
		w.Header().Set("Subscription-Userinfo", header)
	}
	if r.Method == http.MethodHead {
		return
	}
	// variant=raw serves the provider's own config, before the Clash template.
	// It falls back to the templated output when the raw copy is unavailable
	// (e.g. rows cached before raw_yaml existed), so the URL never 404s.
	if r.URL.Query().Get("variant") == "raw" && result.RawYAML != "" {
		_, _ = io.WriteString(w, result.RawYAML)
		return
	}
	_, _ = io.WriteString(w, result.YAML)
}

func formatUserInfo(info *model.UserInfo) string {
	if info == nil {
		return ""
	}
	return fmt.Sprintf("upload=%d; download=%d; total=%d; expire=%d", info.Upload, info.Download, info.Total, info.Expire)
}

// hostOnly returns the hostname of a URL, or "" when it does not parse.
func hostOnly(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return parsed.Hostname()
}

// profileFilename builds the Content-Disposition value clients use as the
// profile name: the configured account name, falling back to the source host.
// A plain ASCII fallback keeps the header valid when the name is Chinese, with
// filename* carrying the real one.
func profileFilename(label, host string) string {
	clean := func(value string) string {
		value = strings.Map(func(r rune) rune {
			if r == '"' || r < 0x20 || r == 0x7f {
				return -1
			}
			return r
		}, value)
		return strings.TrimSpace(value)
	}
	label, host = clean(label), clean(host)
	name := label
	if name == "" {
		name = host
	}
	if name == "" {
		name = "fastlink"
	}
	ascii := strings.Trim(strings.TrimSpace(strings.Map(func(r rune) rune {
		if r > 0x7f {
			return -1
		}
		return r
	}, name)), "/ ")
	if ascii == "" {
		ascii = host
	}
	if ascii == "" {
		ascii = "fastlink"
	}
	value := fmt.Sprintf("attachment; filename=%q", ascii+".yaml")
	if ascii != name {
		value += "; filename*=UTF-8''" + url.PathEscape(name+".yaml")
	}
	return value
}

func extractTarget(input string) (string, error) {
	raw := strings.TrimSpace(input)
	if raw == "" {
		return "", errors.New("请填写订阅链接")
	}
	// http:// must be accepted: the panel's own subscription addresses are served
	// over plain http on the LAN, and the tool is the documented way to inspect
	// them. Rejecting them here made the tool unusable for local addresses.
	if strings.HasPrefix(raw, "https://") || strings.HasPrefix(raw, "http://") {
		return raw, nil
	}
	if parsed, err := url.Parse(raw); err == nil {
		if value := decodedQuery(parsed, "b64"); value != "" {
			return value, nil
		}
		if value := parsed.Query().Get("url"); value != "" {
			return value, nil
		}
	}
	if decoded, err := decodeBase64(raw); err == nil {
		if strings.HasPrefix(decoded, "https://") || strings.HasPrefix(decoded, "http://") {
			return decoded, nil
		}
	}
	return "", errors.New("无法识别订阅链接，请粘贴 http(s):// 开头的地址或 /sub?b64=… 链接")
}

func extractTargetRequest(r *http.Request) (string, error) {
	if r.URL.Query().Has("b64") {
		decoded, err := decodeBase64(r.URL.Query().Get("b64"))
		if err != nil {
			return "", err
		}
		return decoded, nil
	}
	target := strings.TrimSpace(r.URL.Query().Get("url"))
	if target == "" {
		return "", errors.New("missing subscription url")
	}
	return target, nil
}

func decodedQuery(u *url.URL, key string) string {
	value := u.Query().Get(key)
	if value == "" {
		return ""
	}
	decoded, err := decodeBase64(value)
	if err != nil {
		return ""
	}
	return decoded
}

func decodeBase64(value string) (string, error) {
	normalized := strings.NewReplacer("-", "+", "_", "/", " ", "+").Replace(value)
	normalized = strings.Join(strings.Fields(normalized), "")
	if pad := len(normalized) % 4; pad != 0 {
		normalized += strings.Repeat("=", 4-pad)
	}
	raw, err := base64.StdEncoding.DecodeString(normalized)
	return string(raw), err
}