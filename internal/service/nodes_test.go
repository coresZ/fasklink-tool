package service

import "testing"

func TestExtractNodesFlowQuoted(t *testing.T) {
	yaml := "proxies:\n" +
		"    - { name: 'HongKong 01 | x3.0', type: ss, server: x1.example.com, port: 15411 }\n" +
		"    - { name: \"Tokyo 02\", type: ss, server: x2.example.com, port: 15412 }\n" +
		"    - { name: Bare, type: ss, server: x3.example.com, port: 15413 }\n" +
		"proxy-groups:\n  - name: Proxy\n"
	nodes := ExtractNodes(yaml)
	if len(nodes) != 3 {
		t.Fatalf("got %d nodes: %+v", len(nodes), nodes)
	}
	if nodes[0].Name != "HongKong 01 | x3.0" || nodes[0].Server != "x1.example.com" {
		t.Fatalf("unexpected first node: %+v", nodes[0])
	}
	if nodes[1].Name != "Tokyo 02" || nodes[1].Server != "x2.example.com" {
		t.Fatalf("unexpected second node: %+v", nodes[1])
	}
	if nodes[2].Name != "Bare" || nodes[2].Server != "x3.example.com" {
		t.Fatalf("unexpected third node: %+v", nodes[2])
	}
}

func TestExtractNodesJSONFlowStillWorks(t *testing.T) {
	yaml := "proxies:\n" +
		"  - {\"name\":\"台湾-01\",\"type\":\"vless\",\"server\":\"a.example.com\",\"port\":1}\n" +
		"proxy-groups:\n"
	nodes := ExtractNodes(yaml)
	if len(nodes) != 1 || nodes[0].Name != "台湾-01" || nodes[0].Server != "a.example.com" {
		t.Fatalf("unexpected nodes: %+v", nodes)
	}
}

func TestExtractNodesBlockStillWorks(t *testing.T) {
	yaml := "proxies:\n" +
		"  - name: \"香港 01\"\n    server: a.example.com\n" +
		"  - name: 日本 02\n    server: b.example.com\n" +
		"proxy-groups:\n"
	nodes := ExtractNodes(yaml)
	if len(nodes) != 2 {
		t.Fatalf("got %d nodes: %+v", len(nodes), nodes)
	}
	if nodes[1].Name != "日本 02" || nodes[1].Server != "b.example.com" {
		t.Fatalf("unexpected node: %+v", nodes[1])
	}
}

func TestExtractNodesKeepsCommasInsideQuotes(t *testing.T) {
	yaml := "proxies:\n  - { name: 'A, B', type: ss, server: x.example.com }\nproxy-groups:\n"
	nodes := ExtractNodes(yaml)
	if len(nodes) != 1 || nodes[0].Name != "A, B" {
		t.Fatalf("comma split wrongly: %+v", nodes)
	}
}

func TestExtractNodesSkipsEntriesWithoutName(t *testing.T) {
	yaml := "proxies:\n  - { type: ss, server: x.example.com }\nproxy-groups:\n"
	if nodes := ExtractNodes(yaml); len(nodes) != 0 {
		t.Fatalf("expected none, got %+v", nodes)
	}
}

func TestCountNodesMatchesExtractNodes(t *testing.T) {
	yaml := "proxies:\n" +
		"  - { name: 'a', type: ss, server: a.example.com }\n" +
		"  - { name: 'b', type: ss, server: b.example.com }\n" +
		"proxy-groups:\n"
	if got := CountNodes(yaml); got != len(ExtractNodes(yaml)) {
		t.Fatalf("count %d != extracted %d", got, len(ExtractNodes(yaml)))
	}
}
