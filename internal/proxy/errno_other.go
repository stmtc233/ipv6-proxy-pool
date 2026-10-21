//go:build !windows

package proxy

import "syscall"

var (
	errConnRefused = syscall.ECONNREFUSED
	errNetUnreach  = syscall.ENETUNREACH
	errHostUnreach = syscall.EHOSTUNREACH
	errConnReset   = syscall.ECONNRESET
	errBrokenPipe  = syscall.EPIPE
)
