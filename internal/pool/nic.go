package pool

import (
	"errors"
	"net"
)

// NIC 是从网卡上已有地址中选择出口的地址来源
type NIC struct {
	addrs []*net.TCPAddr
}

// NewNIC 预先把地址转换为 *net.TCPAddr，避免每个连接重复解析
func NewNIC(ips []net.IP) (*NIC, error) {
	if len(ips) == 0 {
		return nil, errors.New("no usable IPv6 address on the interface")
	}
	addrs := make([]*net.TCPAddr, len(ips))
	for i, ip := range ips {
		addrs[i] = &net.TCPAddr{IP: ip}
	}
	return &NIC{addrs: addrs}, nil
}

func (n *NIC) Pick(k Key) *net.TCPAddr {
	return n.addrs[k.Lo%uint64(len(n.addrs))]
}

func (n *NIC) Len() int {
	return len(n.addrs)
}
