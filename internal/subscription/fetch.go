package subscription

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"fastlink-tool/internal/model"
)

const MaxProfileBytes = 4 * 1024 * 1024

const (
	SubscriptionUA = "securitynet/v3.1.8 clash-verge Platform/windows"
)

type Fetcher struct {
	Client       *http.Client
	AllowPrivate bool
	// Policy overrides the default host check. When nil, StrictPolicy applies
	// unless AllowPrivate is set.
	Policy HostPolicy
}

// NewFetcher returns a fetcher using the strict host policy.
func NewFetcher(timeout time.Duration) *Fetcher {
	return &Fetcher{Client: &http.Client{
		Timeout: timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 6 {
				return errors.New("too many redirects")
			}
			return nil
		},
	}}
}

// NewLoopbackFetcher returns a fetcher that may reach loopback/private hosts over
// http or https. Used only by the decrypt tool, where the user pastes a URL they
// already control (typically the panel's own LAN address) and the request never
// leaves the machine.
func NewLoopbackFetcher(timeout time.Duration) *Fetcher {
	f := NewFetcher(timeout)
	f.Policy = func(parsed *url.URL) error {
		if !strings.EqualFold(parsed.Scheme, "http") && !strings.EqualFold(parsed.Scheme, "https") {
			return errors.New("只允许 http(s) 订阅地址")
		}
		if parsed.User != nil {
			return errors.New("订阅地址不能包含账号密码")
		}
		return nil
	}
	return f
}

type HTTPStatusError struct {
	Status int
	Detail string
}

func (e *HTTPStatusError) Error() string {
	if e.Detail == "" {
		return fmt.Sprintf("HTTP %d", e.Status)
	}
	return fmt.Sprintf("HTTP %d / %s", e.Status, e.Detail)
}

func IsStaleSubscription(err error) bool {
	var httpErr *HTTPStatusError
	if errors.As(err, &httpErr) {
		switch httpErr.Status {
		case 400, 401, 403, 404, 410:
			return true
		}
	}
	return strings.Contains(strings.ToLower(err.Error()), "invalid token")
}

// HostPolicy decides whether a subscription URL may be fetched. The default is
// strict (https only, no private hosts); callers that must reach the local panel
// pass a narrower policy instead of disabling the check wholesale.
type HostPolicy func(*url.URL) error

// StrictPolicy is the default: https only, no credentials, no private hosts.
func StrictPolicy(parsed *url.URL) error {
	if !strings.EqualFold(parsed.Scheme, "https") {
		return errors.New("只允许 https 订阅地址")
	}
	if parsed.User != nil {
		return errors.New("订阅地址不能包含账号密码")
	}
	if isPrivateHost(parsed.Hostname()) {
		return errors.New("不允许访问内网地址")
	}
	return nil
}

func assertFetchable(raw string) (*url.URL, error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	if err := StrictPolicy(parsed); err != nil {
		return nil, err
	}
	return parsed, nil
}

func isPrivateHost(host string) bool {
	h := strings.ToLower(strings.Trim(strings.TrimSpace(host), "[]"))
	if h == "localhost" || strings.HasSuffix(h, ".localhost") {
		return true
	}
	if ip := net.ParseIP(h); ip != nil {
		return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsUnspecified()
	}
	return false
}

func readCapped(resp *http.Response) ([]byte, error) {
	if declared := resp.Header.Get("Content-Length"); declared != "" {
		if n, err := strconv.ParseInt(declared, 10, 64); err == nil && n > MaxProfileBytes {
			return nil, errors.New("订阅文件超过 4MB")
		}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxProfileBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) > MaxProfileBytes {
		return nil, errors.New("订阅文件超过 4MB")
	}
	return body, nil
}

// ReadCapped is the exported form used by the account login flow.
func ReadCapped(resp *http.Response) ([]byte, error) { return readCapped(resp) }

func httpError(resp *http.Response, body []byte) error {
	detail := strings.TrimSpace(string(body))
	if detail != "" {
		var obj map[string]any
		if json.Unmarshal(body, &obj) == nil {
			for _, key := range []string{"message", "msg", "error"} {
				if value, ok := obj[key]; ok {
					detail = fmt.Sprint(value)
					break
				}
			}
		}
	}
	detail = strings.Join(strings.Fields(detail), " ")
	if len(detail) > 160 {
		detail = detail[:157] + "..."
	}
	return &HTTPStatusError{Status: resp.StatusCode, Detail: detail}
}

// ErrorFromResponse is used by account login for API errors.
func ErrorFromResponse(resp *http.Response, body []byte) error { return httpError(resp, body) }

// FetchSubscription downloads one subscription URL with the required client
// User-Agent and a 4 MB cap.
func (f *Fetcher) FetchSubscription(ctx context.Context, rawURL string) ([]byte, string, error) {
	target, err := url.Parse(rawURL)
	if err != nil {
		return nil, "", err
	}
	check := f.Policy
	if check == nil {
		if f.AllowPrivate {
			check = func(parsed *url.URL) error {
				if parsed.User != nil {
					return errors.New("订阅地址不能包含账号密码")
				}
				return nil
			}
		} else {
			check = StrictPolicy
		}
	}
	if err := check(target); err != nil {
		return nil, "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("User-Agent", SubscriptionUA)
	resp, err := f.Client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	body, err := readCapped(resp)
	if err != nil {
		return nil, "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", httpError(resp, body)
	}
	return body, resp.Header.Get("Subscription-Userinfo"), nil
}

func ParseUserInfo(value string) *model.UserInfo {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	out := &model.UserInfo{}
	found := false
	for _, part := range strings.Split(value, ";") {
		kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(kv) != 2 {
			continue
		}
		n, err := strconv.ParseInt(strings.TrimSpace(kv[1]), 10, 64)
		if err != nil {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(kv[0])) {
		case "upload":
			out.Upload, found = n, true
		case "download":
			out.Download, found = n, true
		case "total":
			out.Total, found = n, true
		case "expire":
			out.Expire, found = n, true
		}
	}
	if !found {
		return nil
	}
	return out
}

func FormatUserInfo(info *model.UserInfo) string {
	if info == nil {
		return ""
	}
	return fmt.Sprintf("upload=%d; download=%d; total=%d; expire=%d", info.Upload, info.Download, info.Total, info.Expire)
}
