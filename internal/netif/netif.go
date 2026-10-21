// Package netif 负责读取和修改网卡上的 IPv6 地址。
package netif

import (
	"bytes"
	"crypto/rand"
	"fmt"
	"net"
	"os/exec"
	"strings"
	"sync"
)

// GlobalIPv6 返回网卡上 2000::/4 范围内的公网 IPv6 地址（含前缀长度）
func GlobalIPv6(name string) ([]*net.IPNet, error) {
	iface, err := net.InterfaceByName(name)
	if err != nil {
		return nil, fmt.Errorf("Failed to obtain network interface: %v", err)
	}
	addrs, err := iface.Addrs()
	if err != nil {
		return nil, fmt.Errorf("Failed to obtain address information: %v", err)
	}

	var r []*net.IPNet
	for _, addr := range addrs {
		ipnet, ok := addr.(*net.IPNet)
		if !ok || ipnet.IP.To4() != nil || len(ipnet.IP) != net.IPv6len || ipnet.IP.IsLoopback() {
			continue
		}
		if ipnet.IP[0]>>4 == 2 {
			r = append(r, ipnet)
		}
	}
	return r, nil
}

// GenerateInPrefix64 生成 count 个与 base 同一 /64 前缀的随机地址
func GenerateInPrefix64(base net.IP, count int) []net.IP {
	base = base.To16()
	if base == nil || count <= 0 {
		return nil
	}
	suffixes := make([]byte, 8*count)
	rand.Read(suffixes)

	ips := make([]net.IP, count)
	for i := range ips {
		ip := make(net.IP, net.IPv6len)
		copy(ip, base[:8])
		copy(ip[8:], suffixes[i*8:])
		ips[i] = ip
	}
	return ips
}

func run(name string, args ...string) error {
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %s: %v: %s", name, strings.Join(args, " "), err, bytes.TrimSpace(out))
	}
	return nil
}

const workers = 8

// runEach 用固定数量的 worker 并发执行 n 个操作。progress 可能被并发调用。
// 单个操作失败不会中断其它操作，最后汇总返回。
func runEach(n int, progress func(int), fn func(i int) error) error {
	jobs := make(chan int)
	var (
		wg     sync.WaitGroup
		mu     sync.Mutex
		failed int
		first  error
	)
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				if err := fn(i); err != nil {
					mu.Lock()
					if failed++; first == nil {
						first = err
					}
					mu.Unlock()
				}
				progress(1)
			}
		}()
	}
	for i := range n {
		jobs <- i
	}
	close(jobs)
	wg.Wait()

	if failed > 0 {
		return fmt.Errorf("%d of %d operations failed, first error: %v", failed, n, first)
	}
	return nil
}
