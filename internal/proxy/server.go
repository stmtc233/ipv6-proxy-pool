// Package proxy 实现 SOCKS5 代理服务端：握手、按地址池选择出口拨号、双向转发。
package proxy

import (
	"context"
	"errors"
	"io"
	"log"
	"net"
	"strconv"
	"time"

	"gosdl/internal/pool"
	"gosdl/internal/socks5"
)

type Server struct {
	Source   pool.Source
	Selector pool.Selector
	Resolver *Resolver
	Hosts    *Hosts // 自定义解析，优先于 DNS；可为 nil

	RequireAuth bool // 只接受用户名/密码认证（hash 选择策略需要凭据）
	AllowIPv4   bool // 允许 IPv4 目标（IPv4 字面量、只有 A 记录的域名），走系统默认 IPv4 出口

	HandshakeTimeout time.Duration // 握手阶段的读写超时，防止慢连接占用资源
	DialTimeout      time.Duration // 连接目标的总超时（含多个 AAAA 地址的重试）
	KeepAlive        time.Duration // 两端 TCP keepalive 周期，用于发现死连接
}

// Serve 循环接受连接。Accept 出错（例如文件描述符耗尽）时指数退避，避免空转占满 CPU。
func (s *Server) Serve(ln net.Listener) error {
	var delay time.Duration
	for {
		conn, err := ln.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return err
			}
			if delay == 0 {
				delay = 5 * time.Millisecond
			} else if delay *= 2; delay > time.Second {
				delay = time.Second
			}
			log.Printf("Error accepting client connection: %v; retrying in %v", err, delay)
			time.Sleep(delay)
			continue
		}
		delay = 0
		go s.handle(conn.(*net.TCPConn))
	}
}

func (s *Server) handle(client *net.TCPConn) {
	defer client.Close()
	client.SetKeepAlive(true)
	client.SetKeepAlivePeriod(s.KeepAlive)

	client.SetDeadline(time.Now().Add(s.HandshakeTimeout))
	req, err := socks5.ReadRequest(client, s.RequireAuth)
	if err != nil {
		logErr("Error reading SOCKS request", err)
		return
	}

	dest, code, err := s.dial(req, client.RemoteAddr())
	if err != nil {
		socks5.WriteReply(client, code, nil)
		log.Printf("Error connecting to %s: %v", req.Address(), err)
		return
	}
	defer dest.Close()

	if err := socks5.WriteReply(client, socks5.ReplySucceeded, dest.LocalAddr().(*net.TCPAddr)); err != nil {
		logErr("Error writing to client", err)
		return
	}
	// 握手完成后取消超时，长连接不会再被强制断开；死连接交给 keepalive 发现
	client.SetDeadline(time.Time{})

	relay(client, dest)
}

func (s *Server) dial(req *socks5.Request, from net.Addr) (*net.TCPConn, byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), s.DialTimeout)
	defer cancel()

	ips, v4, err := s.resolve(ctx, req.Host)
	if err != nil {
		if errors.Is(err, errIPv4Disabled) {
			return nil, socks5.ReplyAddrTypeNotSupported, err
		}
		return nil, socks5.ReplyHostUnreachable, err
	}

	network := "tcp6"
	d := net.Dialer{KeepAlive: s.KeepAlive}
	if v4 {
		// 地址池只有 IPv6，IPv4 走系统默认出口
		network = "tcp4"
	} else {
		d.LocalAddr = s.Source.Pick(s.Selector.Key(&pool.Client{
			Username: req.Username,
			Password: req.Password,
			Addr:     from,
		}))
		if c, ok := s.Source.(pool.Controller); ok {
			d.Control = c.Control
		}
	}
	port := strconv.Itoa(req.Port)

	var lastErr error
	for i, ip := range ips {
		// 和标准库一样，把剩余时间平均分给剩下的地址，避免第一个地址耗尽全部超时
		attemptCtx, attemptCancel := ctx, context.CancelFunc(func() {})
		if deadline, ok := ctx.Deadline(); ok && i < len(ips)-1 {
			attemptCtx, attemptCancel = context.WithTimeout(ctx, time.Until(deadline)/time.Duration(len(ips)-i))
		}
		conn, err := d.DialContext(attemptCtx, network, net.JoinHostPort(ip.String(), port))
		attemptCancel()
		if err == nil {
			return conn.(*net.TCPConn), socks5.ReplySucceeded, nil
		}
		lastErr = err
		if ctx.Err() != nil {
			break
		}
	}
	return nil, replyCode(lastErr), lastErr
}

var errIPv4Disabled = errors.New("IPv4 destination is not supported (set ipv4 = on to allow)")

// resolve 返回目标地址以及是否为 IPv4。有 IPv6 地址时总是使用 IPv6（走地址池）；
// 只有在 AllowIPv4 时，IPv4 字面量和只有 IPv4 地址的域名才会以 IPv4 返回。
// 解析顺序：IP 字面量 > hosts 文件 > DNS。
func (s *Server) resolve(ctx context.Context, host string) ([]net.IP, bool, error) {
	if v6, v4, ok := s.Hosts.Lookup(host); ok {
		switch {
		case len(v6) > 0:
			return v6, false, nil
		case !s.AllowIPv4:
			return nil, true, errIPv4Disabled
		default:
			return v4, true, nil
		}
	}
	if ip := net.ParseIP(host); ip != nil {
		if ip.To4() == nil {
			return []net.IP{ip}, false, nil
		}
		if !s.AllowIPv4 {
			return nil, true, errIPv4Disabled
		}
		return []net.IP{ip}, true, nil
	}

	ips, err := s.Resolver.Lookup(ctx, "ip6", host)
	if err == nil || !s.AllowIPv4 {
		return ips, false, err
	}
	if ips4, err4 := s.Resolver.Lookup(ctx, "ip4", host); err4 == nil {
		return ips4, true, nil
	}
	return nil, false, err
}

func replyCode(err error) byte {
	var ne net.Error
	switch {
	case errors.Is(err, errConnRefused):
		return socks5.ReplyConnectionRefused
	case errors.Is(err, errNetUnreach):
		return socks5.ReplyNetworkUnreachable
	case errors.Is(err, errHostUnreach), errors.As(err, &ne) && ne.Timeout():
		return socks5.ReplyHostUnreachable
	}
	return socks5.ReplyGeneralFailure
}

// relay 双向转发。两端都是 *net.TCPConn，io.Copy 在 Linux 上会走 splice 零拷贝，
// 因此不要给连接再包一层。
func relay(client, dest *net.TCPConn) {
	done := make(chan struct{})
	go func() {
		pipe(dest, client, "Error copying from client to destination")
		close(done)
	}()
	pipe(client, dest, "Error copying from destination to client")
	<-done
}

func pipe(dst, src *net.TCPConn, msg string) {
	if _, err := io.Copy(dst, src); err != nil {
		logErr(msg, err)
		// 异常断开：关闭两端，让另一个方向立即退出
		src.Close()
		dst.Close()
		return
	}
	// 正常读到 EOF：半关闭，把 FIN 传给对端，另一个方向继续转发
	dst.CloseWrite()
}

// logErr 忽略连接正常关闭/被重置这类高频且无意义的错误，避免高并发时日志成为瓶颈
func logErr(msg string, err error) {
	if errors.Is(err, io.EOF) ||
		errors.Is(err, net.ErrClosed) ||
		errors.Is(err, errConnReset) ||
		errors.Is(err, errBrokenPipe) {
		return
	}
	log.Printf("%s: %v", msg, err)
}
