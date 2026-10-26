//go:build windows

package platform

import (
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
)

// FirewallInfo describes whether Windows Firewall is likely to block inbound
// access to the panel. Advisory only: nothing here changes firewall state.
type FirewallInfo struct {
	Supported       bool     `json:"supported"`
	ProfilesEnabled []string `json:"profilesEnabled"`
	RuleFound       bool     `json:"ruleFound"`
	Detail          string   `json:"detail"`
}

// InspectFirewall reads firewall state via netsh.
//
// netsh is used instead of the Get-NetFirewall* cmdlets because starting PowerShell
// costs ~2.4s per call, which made the settings page feel broken. netsh answers in
// ~200ms. The trade-off is localized text, handled by matching the profile names in
// both Chinese and English.
//
// The result is cached briefly: the settings page is opened repeatedly and firewall
// state changes rarely.
const firewallTTL = 30 * time.Second

var (
	firewallMu     sync.Mutex
	firewallCached FirewallInfo
	firewallAt     time.Time
)

func InspectFirewall(int) FirewallInfo {
	firewallMu.Lock()
	defer firewallMu.Unlock()
	if time.Since(firewallAt) < firewallTTL {
		return firewallCached
	}
	firewallCached = inspectFirewallUncached()
	firewallAt = time.Now()
	return firewallCached
}

// InvalidateFirewallCache forces the next InspectFirewall to re-read state.
func InvalidateFirewallCache() {
	firewallMu.Lock()
	firewallAt = time.Time{}
	firewallMu.Unlock()
}

// RuleName is the firewall rule this project manages. Kept here so the inspector and
// scripts/firewall.ps1 cannot drift apart.
const RuleName = "FastLink-tool Panel"

// netsh runs a netsh query with its console window suppressed.
//
// The release binary is built for the GUI subsystem, so every child process that
// owns a console would otherwise flash a black window; the two netsh calls per
// settings read looked like a stutter of cmd popups.
func netsh(args ...string) ([]byte, error) {
	cmd := exec.Command("netsh", args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return cmd.Output()
}

func inspectFirewallUncached() FirewallInfo {
	info := FirewallInfo{Supported: true}

	state, err := netsh("advfirewall", "show", "allprofiles", "state")
	if err != nil {
		info.Detail = "无法读取防火墙状态：" + err.Error()
		return info
	}
	info.ProfilesEnabled = enabledProfiles(string(state))

	// Querying by exact name keeps unrelated FastLink client rules (sing-tun,
	// FastLink_Lite) from being mistaken for this panel's rule.
	rule, ruleErr := netsh("advfirewall", "firewall", "show", "rule", "name="+RuleName, "dir=in")
	if ruleErr == nil {
		text := string(rule)
		// netsh exits 0 and prints "没有与指定标准相匹配的规则" when nothing matches.
		info.RuleFound = !containsAny(text, "没有与指定标准相匹配", "No rules match", "no rules match")
	}

	switch {
	case len(info.ProfilesEnabled) == 0:
		info.Detail = "防火墙配置文件均已关闭，局域网可直接访问。"
	case info.RuleFound:
		info.Detail = "防火墙已启用，且存在面板入站规则。"
	default:
		info.Detail = "防火墙已启用但没有面板入站规则，局域网设备可能无法访问。以管理员身份运行 scripts\\firewall.ps1 -Apply 可创建。"
	}
	return info
}

// enabledProfiles extracts the profiles that are currently ON. netsh prints a
// localized header followed by "状态: 启用" / "State: ON" style lines.
func enabledProfiles(out string) []string {
	var result []string
	current := ""
	for _, raw := range strings.Split(out, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		lower := strings.ToLower(line)
		switch {
		case strings.Contains(line, "域") || strings.Contains(lower, "domain"):
			current = "Domain"
			continue
		case strings.Contains(line, "专用") || strings.Contains(lower, "private"):
			current = "Private"
			continue
		case strings.Contains(line, "公用") || strings.Contains(lower, "public"):
			current = "Public"
			continue
		}
		if current == "" {
			continue
		}
		on := strings.Contains(line, "启用") || strings.Contains(lower, "on")
		off := strings.Contains(line, "关闭") || strings.Contains(lower, "off")
		if on && !off {
			result = append(result, current)
			current = ""
		} else if off {
			current = ""
		}
	}
	return result
}

func containsAny(text string, needles ...string) bool {
	for _, n := range needles {
		if strings.Contains(text, n) {
			return true
		}
	}
	return false
}
