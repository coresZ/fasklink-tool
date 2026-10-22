//go:build !windows

package platform

// FirewallInfo describes whether the host firewall is likely to block inbound
// access to the panel. On non-Windows platforms this is not inspected: the user
// manages ufw/firewalld/pf directly and the panel does not pretend to know.
type FirewallInfo struct {
	Supported       bool     `json:"supported"`
	ProfilesEnabled []string `json:"profilesEnabled"`
	RuleFound       bool     `json:"ruleFound"`
	Detail          string   `json:"detail"`
}

func InspectFirewall(int) FirewallInfo {
	return FirewallInfo{Supported: false, Detail: "当前平台不支持防火墙自检。"}
}
