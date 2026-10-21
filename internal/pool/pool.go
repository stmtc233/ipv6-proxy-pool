// Package pool 定义出口地址池。
//
// 地址池由两个互相独立的维度组合而成：
//
//	Source（地址来源）                      Selector（选择策略）
//	  nic      添加 IP 到网卡                  sequential  顺序地址
//	  anyipv6  整个前缀直接绑定（仅 Linux）    random      随机地址
//	                                           hash        按用户名/密码计算地址，用于锁定 IP
//
// Selector 只根据客户端信息产出一个 128 位 Key，Source 负责把 Key 映射为具体的本地地址：
// nic 取 addrs[Key.Lo%len]；anyipv6 用 Key 填充前缀之后的主机位（任意前缀长度）。
// 因此新增任意一个维度的实现，都不需要改动另一个维度。
package pool

import (
	"fmt"
	"net"
	"syscall"
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

// Key 是选择器产出的 128 位值
type Key struct {
	Hi, Lo uint64
}

// Client 是选择出口地址时可用的客户端信息
type Client struct {
	Username string // 客户端使用 SOCKS5 用户名/密码认证时才有值
	Password string
	Addr     net.Addr
}

// Source 把 Key 映射为出口地址。实现必须是并发安全的，返回值只读。
type Source interface {
	Pick(k Key) *net.TCPAddr
}

// Controller 由需要在拨号前设置 socket 选项的 Source 实现（例如 anyipv6 需要 freebind）
type Controller interface {
	Control(network, address string, c syscall.RawConn) error
}

// Selector 为每个连接产出一个 Key。实现必须是并发安全的。
type Selector interface {
	Key(c *Client) Key
}

// NewSelector 按名称创建选择策略
func NewSelector(name string) (Selector, error) {
	switch name {
	case SelectSequential:
		return &Sequential{}, nil
	case SelectRandom:
		return Random{}, nil
	case SelectHash:
		return Hash{}, nil
	default:
		return nil, fmt.Errorf("unknown select %q", name)
	}
}
