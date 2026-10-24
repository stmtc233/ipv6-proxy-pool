package proxy

import "golang.org/x/sys/windows"

// Windows 上的网络错误是 Winsock 错误码，syscall.ECONNREFUSED 等常量不会匹配
var (
	errConnRefused = windows.WSAECONNREFUSED
	errNetUnreach  = windows.WSAENETUNREACH
	errHostUnreach = windows.WSAEHOSTUNREACH
	errConnReset   = windows.WSAECONNRESET
	errBrokenPipe  = windows.WSAECONNABORTED
)
