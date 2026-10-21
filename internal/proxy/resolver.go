package proxy

import (
	"context"
	"errors"
	"net"
	"strings"
	"sync"
	"time"
)

// Resolver 是带固定 TTL 的 DNS 查询缓存。标准库解析器本身不缓存，
// 同一域名的大量请求每次都要发一次 DNS 查询。
type Resolver struct {
	TTL        time.Duration
	MaxEntries int

	mu    sync.Mutex
	cache map[string]dnsEntry
}

type dnsEntry struct {
	ips     []net.IP
	err     error // 缓存"没有该类型记录"，避免只有 A 记录的域名每次都重新查询 AAAA
	expires time.Time
}

func NewResolver(ttl time.Duration, maxEntries int) *Resolver {
	return &Resolver{TTL: ttl, MaxEntries: maxEntries, cache: make(map[string]dnsEntry)}
}

// Lookup 按 network（"ip6" 或 "ip4"）查询域名地址，返回的切片只读
func (r *Resolver) Lookup(ctx context.Context, network, host string) ([]net.IP, error) {
	key := network + "/" + strings.ToLower(host)

	r.mu.Lock()
	e, ok := r.cache[key]
	r.mu.Unlock()
	if ok && time.Now().Before(e.expires) {
		return e.ips, e.err
	}

	ips, err := net.DefaultResolver.LookupIP(ctx, network, host)
	var dnsErr *net.DNSError
	if err != nil && !(errors.As(err, &dnsErr) && dnsErr.IsNotFound) {
		return nil, err // 超时等临时错误不缓存
	}

	now := time.Now()
	r.mu.Lock()
	if len(r.cache) >= r.MaxEntries {
		r.evict(now)
	}
	r.cache[key] = dnsEntry{ips: ips, err: err, expires: now.Add(r.TTL)}
	r.mu.Unlock()
	return ips, err
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
