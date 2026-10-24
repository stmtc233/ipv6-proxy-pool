package pool

import (
	"encoding/binary"
	"fmt"
	"net"
)

// ParsePrefix 解析 IPv6 前缀（如 2001:db8::/48），前缀长度必须小于 128
func ParsePrefix(s string) (*net.IPNet, error) {
	_, prefix, err := net.ParseCIDR(s)
	if err != nil {
		return nil, fmt.Errorf("invalid prefix %q: %v", s, err)
	}
	if prefix.IP.To4() != nil {
		return nil, fmt.Errorf("prefix %q is not IPv6", s)
	}
	if ones, _ := prefix.Mask.Size(); ones >= 128 {
		return nil, fmt.Errorf("prefix %q: length must be less than 128", s)
	}
	return prefix, nil
}

// PrefixOf 返回地址所在的前缀（主机位清零）
func PrefixOf(a *net.IPNet) *net.IPNet {
	return &net.IPNet{IP: a.IP.Mask(a.Mask), Mask: a.Mask}
}

// HostBits 返回前缀的主机位数
func HostBits(prefix *net.IPNet) int {
	ones, bits := prefix.Mask.Size()
	return bits - ones
}

// AddrInPrefix 保留 prefix 的网络位，用 k 填充主机位
func AddrInPrefix(prefix *net.IPNet, k Key) net.IP {
	var kb [net.IPv6len]byte
	binary.BigEndian.PutUint64(kb[:8], k.Hi)
	binary.BigEndian.PutUint64(kb[8:], k.Lo)

	base, mask := prefix.IP.To16(), prefix.Mask
	ip := make(net.IP, net.IPv6len)
	for i := range ip {
		ip[i] = base[i]&mask[i] | kb[i]&^mask[i]
	}
	return ip
}

// RandomInPrefix 生成 count 个 prefix 内互不相同、且不在 exclude 中的随机地址。
// 主机位空间不够时（如 /120）只返回剩余可用的数量。
func RandomInPrefix(prefix *net.IPNet, count int, exclude []net.IP) []net.IP {
	seen := make(map[[net.IPv6len]byte]struct{})
	for _, ip := range exclude {
		if prefix.Contains(ip) {
			seen[[net.IPv6len]byte(ip.To16())] = struct{}{}
		}
	}
	// 用 uint64 计算容量，32 位平台上 int 放不下 1<<32 以上的值
	count = max(0, count)
	if hb := HostBits(prefix); hb < 64 {
		if avail := uint64(1)<<hb - uint64(len(seen)); uint64(count) > avail {
			count = int(avail)
		}
	}

	ips := make([]net.IP, 0, count)
	for len(ips) < count {
		ip := AddrInPrefix(prefix, randomKey())
		k := [net.IPv6len]byte(ip)
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		ips = append(ips, ip)
	}
	return ips
}
