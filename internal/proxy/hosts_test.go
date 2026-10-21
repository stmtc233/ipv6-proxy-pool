package proxy

import (
	"strings"
	"testing"
)

func TestParseHosts(t *testing.T) {
	h, err := ParseHosts(strings.NewReader(`
# 注释
2001:db8::1,2001:db8::2 Example.com
2001:db8::3 example.com. www.example.com  # 行尾注释
1.2.3.4,2001:db8::4 mixed.test
5.6.7.8 v4only.test
`))
	if err != nil {
		t.Fatal(err)
	}
	if h.Len() != 4 {
		t.Fatalf("len %d", h.Len())
	}

	v6, v4, ok := h.Lookup("EXAMPLE.COM.")
	if !ok || len(v6) != 3 || len(v4) != 0 {
		t.Fatalf("example.com: %v %v %v", v6, v4, ok)
	}
	if v6, _, _ := h.Lookup("www.example.com"); len(v6) != 1 || v6[0].String() != "2001:db8::3" {
		t.Errorf("www.example.com: %v", v6)
	}
	if v6, v4, _ := h.Lookup("mixed.test"); len(v6) != 1 || len(v4) != 1 || v4[0].String() != "1.2.3.4" {
		t.Errorf("mixed.test: %v %v", v6, v4)
	}
	if _, _, ok := h.Lookup("missing.test"); ok {
		t.Error("missing.test found")
	}
	var nilHosts *Hosts
	if _, _, ok := nilHosts.Lookup("example.com"); ok {
		t.Error("nil hosts found")
	}
}

func TestParseHostsErrors(t *testing.T) {
	for _, in := range []string{
		"2001:db8::1",                    // 缺少域名
		"2001:db8::1,nope example.com",   // 非法地址
		"ok.test\n2001:db8::zz bad.test", // 第一行就缺地址
	} {
		if _, err := ParseHosts(strings.NewReader(in)); err == nil {
			t.Errorf("%q: expected error", in)
		}
	}
}

// 每次请求的首选地址应当是随机的
func TestHostsLookupRandom(t *testing.T) {
	h, _ := ParseHosts(strings.NewReader("2001:db8::1,2001:db8::2,2001:db8::3 r.test"))
	first := map[string]int{}
	for range 300 {
		v6, _, _ := h.Lookup("r.test")
		if len(v6) != 3 {
			t.Fatalf("got %v", v6)
		}
		first[v6[0].String()]++
	}
	if len(first) != 3 {
		t.Errorf("first address distribution %v", first)
	}
	// 打乱的是副本，不影响原表
	if h.m["r.test"].v6[0].String() != "2001:db8::1" {
		t.Error("original entry was modified")
	}
}
