package pool

import (
	"net"
	"testing"
)

func mustPrefix(t *testing.T, s string) *net.IPNet {
	t.Helper()
	p, err := ParsePrefix(s)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestParsePrefix(t *testing.T) {
	p := mustPrefix(t, "2001:db8:1:2:3::1/48") // 主机位会被清零
	if p.String() != "2001:db8:1::/48" {
		t.Errorf("got %s", p)
	}
	for _, bad := range []string{"2001:db8::1/128", "10.0.0.0/8", "2001:db8::1", "x"} {
		if _, err := ParsePrefix(bad); err == nil {
			t.Errorf("%q: expected error", bad)
		}
	}
}

func TestAddrInPrefix(t *testing.T) {
	full := Key{Hi: ^uint64(0), Lo: ^uint64(0)}
	cases := []struct {
		prefix string
		key    Key
		want   string
	}{
		{"2001:db8::/64", Key{Lo: 1}, "2001:db8::1"},
		{"2001:db8::/64", full, "2001:db8::ffff:ffff:ffff:ffff"},
		{"2001:db8::/48", full, "2001:db8:0:ffff:ffff:ffff:ffff:ffff"}, // 主机位超过 64 位时用到 Hi
		{"2001:db8::/56", Key{Hi: 0xab, Lo: 2}, "2001:db8:0:ab::2"},
		{"2001:db8::/60", full, "2001:db8:0:f:ffff:ffff:ffff:ffff"}, // 非 16 位边界
		{"2001:db8::/112", full, "2001:db8::ffff"},
		{"2001:db8::/127", full, "2001:db8::1"},
	}
	for _, c := range cases {
		got := AddrInPrefix(mustPrefix(t, c.prefix), c.key).String()
		if got != c.want {
			t.Errorf("%s %+v: got %s want %s", c.prefix, c.key, got, c.want)
		}
	}
}

func TestRandomInPrefix(t *testing.T) {
	p := mustPrefix(t, "2001:db8::/120")
	exclude := []net.IP{net.ParseIP("2001:db8::1"), net.ParseIP("2001:db9::1")}
	ips := RandomInPrefix(p, 1000, exclude) // 只有 256 个主机位置，其中 1 个被排除
	if len(ips) != 255 {
		t.Fatalf("got %d addresses", len(ips))
	}
	seen := map[string]bool{}
	for _, ip := range ips {
		if !p.Contains(ip) || ip.Equal(exclude[0]) || seen[ip.String()] {
			t.Fatalf("bad address %s", ip)
		}
		seen[ip.String()] = true
	}

	p = mustPrefix(t, "2001:db8::/48")
	for _, ip := range RandomInPrefix(p, 100, nil) {
		if !p.Contains(ip) {
			t.Fatalf("%s not in %s", ip, p)
		}
	}
}

func TestSelectors(t *testing.T) {
	var s Sequential
	for i := range uint64(3) {
		if k := s.Key(nil); k != (Key{Lo: i}) {
			t.Fatalf("sequential %d: %+v", i, k)
		}
	}

	var h Hash
	a := h.Key(&Client{Username: "alice", Password: "x"})
	if a != h.Key(&Client{Username: "alice", Password: "x"}) {
		t.Error("hash is not stable")
	}
	if a == h.Key(&Client{Username: "bob", Password: "x"}) {
		t.Error("different users got the same key")
	}
	if h.Key(&Client{Username: "ab", Password: "c"}) == h.Key(&Client{Username: "a", Password: "bc"}) {
		t.Error("username/password boundary is ambiguous")
	}

	var r Random
	if r.Key(nil) == r.Key(nil) {
		t.Error("random returned the same key twice")
	}

	nic, _ := NewNIC([]net.IP{net.ParseIP("2001:db8::1"), net.ParseIP("2001:db8::2")})
	if !nic.Pick(Key{Lo: 3}).IP.Equal(net.ParseIP("2001:db8::2")) {
		t.Error("nic pick")
	}
}
