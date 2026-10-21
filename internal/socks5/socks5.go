// Package socks5 实现 SOCKS5 协议（RFC 1928）握手与应答的解析。
package socks5

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
)

const Version5 = 0x05

// 认证方式
const (
	MethodNoAuth       = 0x00
	MethodUserPass     = 0x02 // 预留：用户名/密码认证（RFC 1929）
	MethodNoAcceptable = 0xFF
)

const CmdConnect = 0x01

// 地址类型
const (
	AtypIPv4   = 0x01
	AtypDomain = 0x03
	AtypIPv6   = 0x04
)

// 应答码
const (
	ReplySucceeded            = 0x00
	ReplyGeneralFailure       = 0x01
	ReplyNotAllowed           = 0x02
	ReplyNetworkUnreachable   = 0x03
	ReplyHostUnreachable      = 0x04
	ReplyConnectionRefused    = 0x05
	ReplyTTLExpired           = 0x06
	ReplyCommandNotSupported  = 0x07
	ReplyAddrTypeNotSupported = 0x08
)

var ErrNoAcceptableMethod = errors.New("socks5: no acceptable authentication method")

// Request 是一次完整握手后得到的连接请求
type Request struct {
	// 预留：启用用户名/密码认证后填充，供"按用户名/密码锁定 IP"的选择器使用
	Username string
	Password string

	Cmd      byte
	AddrType byte
	Host     string // 域名或 IP 字面量
	Port     int
}

// Address 返回 host:port 形式的目标地址
func (r *Request) Address() string {
	return net.JoinHostPort(r.Host, strconv.Itoa(r.Port))
}

// ReadRequest 完成认证协商并读取连接请求。
// 使用 io.ReadFull 按协议字段长度读取，不依赖单次 Read 拿到完整报文。
func ReadRequest(rw io.ReadWriter) (*Request, error) {
	req := &Request{}
	if err := negotiate(rw, req); err != nil {
		return nil, err
	}

	var hdr [4]byte
	if _, err := io.ReadFull(rw, hdr[:]); err != nil {
		return nil, err
	}
	if hdr[0] != Version5 {
		return nil, fmt.Errorf("socks5: unsupported version %#x", hdr[0])
	}
	req.Cmd = hdr[1]
	req.AddrType = hdr[3]

	switch req.AddrType {
	case AtypIPv4:
		var b [net.IPv4len]byte
		if _, err := io.ReadFull(rw, b[:]); err != nil {
			return nil, err
		}
		req.Host = net.IP(b[:]).String()
	case AtypDomain:
		var l [1]byte
		if _, err := io.ReadFull(rw, l[:]); err != nil {
			return nil, err
		}
		if l[0] == 0 {
			return nil, errors.New("socks5: empty domain name")
		}
		name := make([]byte, l[0])
		if _, err := io.ReadFull(rw, name); err != nil {
			return nil, err
		}
		req.Host = string(name)
	case AtypIPv6:
		var b [net.IPv6len]byte
		if _, err := io.ReadFull(rw, b[:]); err != nil {
			return nil, err
		}
		req.Host = net.IP(b[:]).String()
	default:
		WriteReply(rw, ReplyAddrTypeNotSupported, nil)
		return nil, fmt.Errorf("socks5: unsupported address type %#x", req.AddrType)
	}

	var port [2]byte
	if _, err := io.ReadFull(rw, port[:]); err != nil {
		return nil, err
	}
	req.Port = int(port[0])<<8 | int(port[1])

	if req.Cmd != CmdConnect {
		WriteReply(rw, ReplyCommandNotSupported, nil)
		return nil, fmt.Errorf("socks5: unsupported command %#x", req.Cmd)
	}
	return req, nil
}

func negotiate(rw io.ReadWriter, req *Request) error {
	var hdr [2]byte
	if _, err := io.ReadFull(rw, hdr[:]); err != nil {
		return err
	}
	if hdr[0] != Version5 {
		return fmt.Errorf("socks5: unsupported version %#x", hdr[0])
	}
	methods := make([]byte, hdr[1])
	if _, err := io.ReadFull(rw, methods); err != nil {
		return err
	}

	// TODO: "按用户名/密码锁定 IP" 模式需要在这里支持 MethodUserPass（RFC 1929），
	// 完成子协商后填充 req.Username / req.Password。
	if bytes.IndexByte(methods, MethodNoAuth) < 0 {
		rw.Write([]byte{Version5, MethodNoAcceptable})
		return ErrNoAcceptableMethod
	}
	_, err := rw.Write([]byte{Version5, MethodNoAuth})
	return err
}

// WriteReply 发送应答。bound 为 nil 或非 IPv6 时以 0.0.0.0:0 作为绑定地址。
func WriteReply(w io.Writer, code byte, bound *net.TCPAddr) error {
	buf := make([]byte, 0, 4+net.IPv6len+2)
	buf = append(buf, Version5, code, 0x00)
	port := 0
	if bound != nil && bound.IP.To4() == nil && len(bound.IP) == net.IPv6len {
		buf = append(buf, AtypIPv6)
		buf = append(buf, bound.IP...)
		port = bound.Port
	} else {
		buf = append(buf, AtypIPv4, 0, 0, 0, 0)
	}
	buf = append(buf, byte(port>>8), byte(port))
	_, err := w.Write(buf)
	return err
}
