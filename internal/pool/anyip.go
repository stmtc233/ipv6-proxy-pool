package pool

import (
	"net"
	"syscall"
)

// AnyIP 直接从整个前缀中选择出口地址，不需要把地址添加到网卡。
// 依赖 freebind 绑定非本机地址，并需要一条 local 路由让回包被本机接收。
type AnyIP struct {
	prefix  *net.IPNet
	control func(network, address string, c syscall.RawConn) error
}

// NewAnyIP 的 control 在每次拨号前调用，用于设置 freebind 等 socket 选项
func NewAnyIP(prefix *net.IPNet, control func(network, address string, c syscall.RawConn) error) *AnyIP {
	return &AnyIP{prefix: prefix, control: control}
}

func (a *AnyIP) Pick(k Key) *net.TCPAddr {
	return &net.TCPAddr{IP: AddrInPrefix(a.prefix, k)}
}

func (a *AnyIP) Control(network, address string, c syscall.RawConn) error {
	return a.control(network, address, c)
}

func (a *AnyIP) Prefix() *net.IPNet {
	return a.prefix
}
