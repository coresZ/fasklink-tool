package subscription

import (
	"net"
	"strings"
)

// maxUpstreamNameservers caps how many provider-supplied resolvers are merged
// in, so a hostile or sloppy profile cannot bloat the dns block.
const maxUpstreamNameservers = 4

// proxyNSMarker is the placeholder line in template.yaml. It is a comment, so
// an unreplaced template still parses.
const proxyNSMarker = "# __SUB_NS__"

// upstreamNameservers returns the provider's own proxy-server-nameserver
// entries from a profile. Both the block form and the inline flow form are
// accepted; anything unrecognised is dropped rather than failing generation.
func upstreamNameservers(yamlText string) []string {
	lines := strings.Split(strings.ReplaceAll(strings.TrimPrefix(yamlText, "\ufeff"), "\r\n", "\n"), "\n")
	var (
		out  []string
		seen = map[string]bool{}
	)
	add := func(raw string) {
		value := trimResolver(raw)
		if value == "" || !validNameserver(value) || seen[value] || len(out) >= maxUpstreamNameservers {
			return
		}
		seen[value] = true
		out = append(out, value)
	}

	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(line, "proxy-server-nameserver:") {
			continue
		}
		rest := strings.TrimSpace(strings.TrimPrefix(line, "proxy-server-nameserver:"))
		if strings.HasPrefix(rest, "[") {
			// Flow form: proxy-server-nameserver: [a, b]
			inner := strings.TrimSuffix(strings.TrimPrefix(rest, "["), "]")
			for _, part := range strings.Split(inner, ",") {
				add(part)
			}
			return out
		}
		if rest != "" {
			add(rest)
		}
		for j := i + 1; j < len(lines); j++ {
			item := strings.TrimSpace(lines[j])
			switch {
			case item == "":
				continue
			case strings.HasPrefix(item, "#"):
				continue
			case strings.HasPrefix(item, "- "):
				add(strings.TrimPrefix(item, "- "))
			case item == "-":
				continue
			default:
				return out
			}
		}
		return out
	}
	return out
}

func trimResolver(raw string) string {
	value := strings.TrimSpace(raw)
	value = strings.Trim(value, `"'`)
	return strings.TrimSpace(value)
}

// validNameserver reports whether value is a resolver mihomo understands:
// https/tls/quic/h3/udp schemes, a bare IP, or "system".
func validNameserver(value string) bool {
	if value == "" || strings.ContainsAny(value, " \t") {
		return false
	}
	if strings.EqualFold(value, "system") {
		return true
	}
	if net.ParseIP(strings.Trim(value, "[]")) != nil {
		return true
	}
	lower := strings.ToLower(value)
	for _, scheme := range []string{"https://", "tls://", "quic://", "h3://", "udp://"} {
		if strings.HasPrefix(lower, scheme) {
			return true
		}
	}
	return false
}

// mergeNameservers replaces the marker line with the provider entries, which go
// ahead of the template defaults. Entries already present in skip are omitted.
func mergeNameservers(text string, extra []string, skip map[string]bool) string {
	idx := strings.Index(text, proxyNSMarker)
	if idx < 0 {
		return text
	}
	start := idx
	for start > 0 && text[start-1] != '\n' {
		start--
	}
	end := idx + len(proxyNSMarker)
	for end < len(text) && text[end] != '\n' {
		end++
	}
	if end < len(text) {
		end++
	}

	var b strings.Builder
	for _, value := range extra {
		if skip[value] {
			continue
		}
		b.WriteString("    - ")
		b.WriteString(value)
		b.WriteString("\n")
	}
	return text[:start] + b.String() + text[end:]
}
