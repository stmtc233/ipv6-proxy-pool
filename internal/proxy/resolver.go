package proxy

import (
	"context"
	"net"
	"strings"
	"sync"
	"time"
)

// Resolver 是带固定 TTL 的 AAAA 查询缓存。标准库解析器本身不缓存，
// 同一域名的大量请求每次都要发一次 DNS 查询。
type Resolver struct {
	TTL        time.Duration
	MaxEntries int

	mu    sync.Mutex
	cache map[string]dnsEntry
}

type dnsEntry struct {
	ips     []net.IP
	expires time.Time
}

func NewResolver(ttl time.Duration, maxEntries int) *Resolver {
	return &Resolver{TTL: ttl, MaxEntries: maxEntries, cache: make(map[string]dnsEntry)}
}

// LookupIP6 返回域名的 IPv6 地址，返回的切片只读
func (r *Resolver) LookupIP6(ctx context.Context, host string) ([]net.IP, error) {
	host = strings.ToLower(host)

	r.mu.Lock()
	e, ok := r.cache[host]
	r.mu.Unlock()
	if ok && time.Now().Before(e.expires) {
		return e.ips, nil
	}

	ips, err := net.DefaultResolver.LookupIP(ctx, "ip6", host)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	r.mu.Lock()
	if len(r.cache) >= r.MaxEntries {
		r.evict(now)
	}
	r.cache[host] = dnsEntry{ips: ips, expires: now.Add(r.TTL)}
	r.mu.Unlock()
	return ips, nil
}

// evict 先清理过期项，仍然超限就整体清空。调用方需持有锁。
func (r *Resolver) evict(now time.Time) {
	for k, e := range r.cache {
		if !now.Before(e.expires) {
			delete(r.cache, k)
		}
	}
	if len(r.cache) >= r.MaxEntries {
		r.cache = make(map[string]dnsEntry)
	}
}
