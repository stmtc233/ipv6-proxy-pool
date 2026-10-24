package proxy

import (
	"syscall"

	"golang.org/x/sys/unix"
)

// bindNoPort 设置 IP_BIND_ADDRESS_NO_PORT（对 IPv6 socket 同样生效）：绑定出口地址时先不分配端口，
// 推迟到 connect 时按四元组分配。否则 bind 会为每个出口 IP 独占一个端口（TIME_WAIT 也占着），
// 单个出口 IP 最多约 2.8 万条出站连接，且 tcp_tw_reuse 不起作用。
// 内核不支持（4.2 以前）时忽略，退回原来的行为。
func bindNoPort(c syscall.RawConn) {
	c.Control(func(fd uintptr) {
		unix.SetsockoptInt(int(fd), unix.IPPROTO_IP, unix.IP_BIND_ADDRESS_NO_PORT, 1)
	})
}
