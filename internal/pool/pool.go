// Package pool 定义出口地址池。
//
// 地址池由两个互相独立的维度组合而成：
//
//	Source（地址来源）                    Selector（选择策略）
//	  nic      添加 IP 到网卡（已实现）        sequential  顺序地址（已实现）
//	  anyipv6  整段前缀直接绑定（预留）        random      随机地址（预留）
//	                                           hash        按用户名/密码计算地址，用于锁定 IP（预留）
//
// Selector 只根据客户端信息产出一个 uint64 key，Source 负责把 key 映射为具体的本地地址：
// nic 取 addrs[key%len]；anyipv6 可以直接把 key 作为 /64 前缀后的 64 位主机号。
// 因此新增任意一个维度的实现，都不需要改动另一个维度。
package pool

import (
	"fmt"
	"net"
)

// 地址来源
const (
	ModeNIC     = "nic"
	ModeAnyIPv6 = "anyipv6"
)

// 选择策略
const (
	SelectSequential = "sequential"
	SelectRandom     = "random"
	SelectHash       = "hash"
)

// Client 是选择出口地址时可用的客户端信息
type Client struct {
	Username string // SOCKS5 用户名/密码认证启用后才有值
	Password string
	Addr     net.Addr
}

// Source 把 key 映射为出口地址。实现必须是并发安全的，返回值只读。
type Source interface {
	Pick(key uint64) *net.TCPAddr
}

// Selector 为每个连接产出一个 key。实现必须是并发安全的。
type Selector interface {
	Key(c *Client) uint64
}

// NewSelector 按名称创建选择策略
func NewSelector(name string) (Selector, error) {
	switch name {
	case SelectSequential:
		return &Sequential{}, nil
	case SelectRandom, SelectHash:
		return nil, fmt.Errorf("select %q is not implemented yet", name)
	default:
		return nil, fmt.Errorf("unknown select %q", name)
	}
}
