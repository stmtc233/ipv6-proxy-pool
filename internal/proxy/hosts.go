package proxy

import (
	"bufio"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"os"
	"strings"
)

// Hosts 是自定义域名解析表，优先于 DNS。
//
// 文件格式，每行一条，# 开头为注释：
//
//	2001:db8::1,2001:db8::2 example.com
//	2001:db8::3 example.org www.example.org
//
// 同一域名可以出现在多行，地址会合并。IPv4 地址只在开启 IPv4 兼容时生效。
type Hosts struct {
	m map[string]*hostEntry
}

type hostEntry struct {
	v6, v4 []net.IP
}

// LoadHosts 读取 hosts 文件
func LoadHosts(path string) (*Hosts, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	h, err := ParseHosts(f)
	if err != nil {
		return nil, fmt.Errorf("%s: %v", path, err)
	}
	return h, nil
}

func ParseHosts(r io.Reader) (*Hosts, error) {
	h := &Hosts{m: make(map[string]*hostEntry)}
	sc := bufio.NewScanner(r)
	for n := 1; sc.Scan(); n++ {
		text := sc.Text()
		if n == 1 {
			text = strings.TrimPrefix(text, "\ufeff") // 部分 Windows 编辑器保存的 UTF-8 带 BOM
		}
		line, _, _ := strings.Cut(text, "#")
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if len(fields) < 2 {
			return nil, fmt.Errorf("line %d: expected \"address[,address...] domain\"", n)
		}

		var e hostEntry
		for _, s := range strings.Split(fields[0], ",") {
			ip := net.ParseIP(strings.TrimSpace(s))
			if ip == nil {
				return nil, fmt.Errorf("line %d: invalid address %q", n, s)
			}
			if ip4 := ip.To4(); ip4 != nil {
				e.v4 = append(e.v4, ip4)
			} else {
				e.v6 = append(e.v6, ip)
			}
		}
		for _, name := range fields[1:] {
			name = normalizeHost(name)
			old := h.m[name]
			if old == nil {
				old = &hostEntry{}
				h.m[name] = old
			}
			old.v6 = append(old.v6, e.v6...)
			old.v4 = append(old.v4, e.v4...)
		}
	}
	return h, sc.Err()
}

// Len 返回域名数量
func (h *Hosts) Len() int {
	if h == nil {
		return 0
	}
	return len(h.m)
}

// Lookup 返回打乱顺序后的地址副本：拨号时第一个地址是随机的，其余作为失败后的备选
func (h *Hosts) Lookup(host string) (v6, v4 []net.IP, ok bool) {
	if h == nil {
		return nil, nil, false
	}
	e, ok := h.m[normalizeHost(host)]
	if !ok {
		return nil, nil, false
	}
	return shuffled(e.v6), shuffled(e.v4), true
}

func normalizeHost(host string) string {
	return strings.TrimSuffix(strings.ToLower(host), ".")
}

func shuffled(ips []net.IP) []net.IP {
	if len(ips) == 0 {
		return nil
	}
	r := append([]net.IP(nil), ips...)
	rand.Shuffle(len(r), func(i, j int) { r[i], r[j] = r[j], r[i] })
	return r
}
