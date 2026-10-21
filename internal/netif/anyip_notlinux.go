//go:build !linux

package netif

import (
	"errors"
	"net"
	"syscall"
)

var errAnyIPUnsupported = errors.New("anyipv6 mode only supports Linux")

func FreeBind(network, address string, c syscall.RawConn) error {
	return errAnyIPUnsupported
}

func AddLocalRoute(prefix *net.IPNet) error {
	return errAnyIPUnsupported
}
