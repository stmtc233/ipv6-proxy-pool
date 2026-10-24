//go:build !linux

package proxy

import "syscall"

func bindNoPort(c syscall.RawConn) {}
