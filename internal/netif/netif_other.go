//go:build !linux && !windows

package netif

import (
	"errors"
	"net"
)

var errUnsupported = errors.New("unsupported system")

func Add(name string, ips []net.IP, progress func(int)) error {
	return errUnsupported
}

func Delete(name string, addrs []*net.IPNet, progress func(int)) error {
	return errUnsupported
}
