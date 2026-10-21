// Package proxy 实现 SOCKS5 代理服务端：握手、按地址池选择出口拨号、双向转发。
package proxy

import (
	"context"
	"errors"
	"io"
	"log"
	"net"
	"strconv"
	"syscall"
	"time"

	"gosdl/internal/pool"
	"gosdl/internal/socks5"
)

type Server struct {
	Source   pool.Source
	Selector pool.Selector
	Resolver *Resolver

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
	req, err := socks5.ReadRequest(client)
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

	var ips []net.IP
	if ip := net.ParseIP(req.Host); ip != nil {
		if ip.To4() != nil {
			return nil, socks5.ReplyAddrTypeNotSupported, errors.New("IPv4 destination is not supported")
		}
		ips = []net.IP{ip}
	} else {
		var err error
		if ips, err = s.Resolver.LookupIP6(ctx, req.Host); err != nil {
			return nil, socks5.ReplyHostUnreachable, err
		}
	}

	local := s.Source.Pick(s.Selector.Key(&pool.Client{
		Username: req.Username,
		Password: req.Password,
		Addr:     from,
	}))
	d := net.Dialer{LocalAddr: local, KeepAlive: s.KeepAlive}
	port := strconv.Itoa(req.Port)

	var lastErr error
	for i, ip := range ips {
		// 和标准库一样，把剩余时间平均分给剩下的地址，避免第一个地址耗尽全部超时
		attemptCtx, attemptCancel := ctx, context.CancelFunc(func() {})
		if deadline, ok := ctx.Deadline(); ok && i < len(ips)-1 {
			attemptCtx, attemptCancel = context.WithTimeout(ctx, time.Until(deadline)/time.Duration(len(ips)-i))
		}
		conn, err := d.DialContext(attemptCtx, "tcp6", net.JoinHostPort(ip.String(), port))
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

func replyCode(err error) byte {
	var ne net.Error
	switch {
	case errors.Is(err, syscall.ECONNREFUSED):
		return socks5.ReplyConnectionRefused
	case errors.Is(err, syscall.ENETUNREACH):
		return socks5.ReplyNetworkUnreachable
	case errors.Is(err, syscall.EHOSTUNREACH), errors.As(err, &ne) && ne.Timeout():
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
		errors.Is(err, syscall.ECONNRESET) ||
		errors.Is(err, syscall.EPIPE) {
		return
	}
	log.Printf("%s: %v", msg, err)
}
