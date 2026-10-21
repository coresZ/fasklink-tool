package subscription

import (
	_ "embed"
	"errors"
	"regexp"
	"strings"
)

//go:embed template.yaml
var templateText string

var (
	controllerLine = regexp.MustCompile(`(?m)^external-controller:.*\n?`)
	secretLine     = regexp.MustCompile(`(?m)^secret:.*\n?`)
)

// ExtractProxiesBlock ports extractProxiesBlock() from transferSub.local.mjs.
func ExtractProxiesBlock(yamlText string) (string, error) {
	text := strings.TrimPrefix(yamlText, "\ufeff")
	text = strings.ReplaceAll(text, "\r\n", "\n")
	lines := strings.Split(text, "\n")
	start := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == "proxies:" {
			start = i
			break
		}
	}
	if start < 0 {
		return "", errors.New("订阅中未找到 proxies 节点列表")
	}
	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		line := lines[i]
		if len(line) > 0 && line[0] != ' ' && line[0] != '\t' && !strings.HasPrefix(strings.TrimSpace(line), "#") {
			end = i
			break
		}
	}
	block := lines[start+1 : end]
	first := ""
	for _, line := range block {
		if strings.TrimSpace(line) != "" && !strings.HasPrefix(strings.TrimSpace(line), "#") {
			first = line
			break
		}
	}
	if first == "" {
		return "", errors.New("proxies 列表为空")
	}
	itemIndent := len(first) - len(strings.TrimLeft(first, " \t"))
	if itemIndent <= 0 || !strings.Contains(first[itemIndent:], "-") || !strings.HasPrefix(first[itemIndent:], "-") {
		return "", errors.New("proxies 不是 YAML 列表")
	}
	var out []string
	for _, line := range block {
		if strings.TrimSpace(line) == "" || strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " \t"))
		if indent < itemIndent || len(line) < itemIndent {
			continue
		}
		out = append(out, "  "+line[itemIndent:])
	}
	if len(out) == 0 {
		return "", errors.New("proxies 列表为空")
	}
	return strings.Join(out, "\n"), nil
}

// ApplyTemplate injects the proxy block and removes controller overrides,
// matching applyClashTemplate() byte-for-byte where practical.
func ApplyTemplate(sourceYAML string) (string, error) {
	proxies, err := ExtractProxiesBlock(sourceYAML)
	if err != nil {
		return "", err
	}
	out := strings.Replace(templateText, "__PROXIES_BLOCK__", proxies, 1)
	out = controllerLine.ReplaceAllString(out, "")
	out = secretLine.ReplaceAllString(out, "")
	return out, nil
}
