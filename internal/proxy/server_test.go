package proxy

import (
	"bytes"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"gosdl/internal/pool"
	"gosdl/internal/socks5"
)

func startProxy(t *testing.T, allowIPv4 bool) string {
	return startProxyWithHosts(t, allowIPv4, nil)
}

func startProxyWithHosts(t *testing.T, allowIPv4 bool, hosts *Hosts) string {
	t.Helper()
	src, err := pool.NewNIC([]net.IP{net.IPv6loopback})
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	srv := &Server{
		Source:           src,
		Selector:         &pool.Sequential{},
		Resolver:         NewResolver(time.Minute, 16),
		AllowIPv4:        allowIPv4,
		Hosts:            hosts,
		HandshakeTimeout: 200 * time.Millisecond,
		DialTimeout:      5 * time.Second, // Windows 收到 RST 后会重试，拒绝连接约需 2 秒
		KeepAlive:        30 * time.Second,
	}
	go srv.Serve(ln)
	return ln.Addr().String()
}

// connect 逐字节发送握手，验证服务端不依赖单次 Read 拿到完整报文
func connect(t *testing.T, proxyAddr string, atyp byte, addr []byte, port int) (net.Conn, byte) {
	t.Helper()
	c, err := net.Dial("tcp", proxyAddr)
	if err != nil {
		t.Fatal(err)
	}
	msg := []byte{5, 1, 0, 5, 1, 0, atyp}
	msg = append(msg, addr...)
	msg = append(msg, byte(port>>8), byte(port))
	for i := 0; i < 3; i++ {
		c.Write(msg[i : i+1])
		time.Sleep(5 * time.Millisecond)
	}
	method := make([]byte, 2)
	if _, err := io.ReadFull(c, method); err != nil || method[1] != 0 {
		t.Fatalf("method reply %v %v", method, err)
	}
	for _, b := range msg[3:] {
		c.Write([]byte{b})
	}
	hdr := make([]byte, 4)
	if _, err := io.ReadFull(c, hdr); err != nil {
		t.Fatalf("reply: %v", err)
	}
	rest := 4 + 2
	if hdr[3] == socks5.AtypIPv6 {
		rest = 16 + 2
	}
	io.ReadFull(c, make([]byte, rest))
	return c, hdr[1]
}

func TestRelayHalfCloseAndNoDeadline(t *testing.T) {
	echo, err := net.Listen("tcp6", "[::1]:0")
	if err != nil {
		t.Skip("no IPv6 loopback:", err)
	}
	port := startEcho(echo)
	c, code := connect(t, startProxy(t, false), socks5.AtypIPv6, net.IPv6loopback, port)
	checkEcho(t, c, code)
}

// IPv4 兼容开启时，IPv4 目标走系统默认出口
func TestIPv4Allowed(t *testing.T) {
	echo, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := startEcho(echo)
	c, code := connect(t, startProxy(t, true), socks5.AtypIPv4, []byte{127, 0, 0, 1}, port)
	checkEcho(t, c, code)
}

// hosts 文件中的域名优先于 DNS 解析
func TestHostsOverride(t *testing.T) {
	echo, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := startEcho(echo)
	hosts, err := ParseHosts(strings.NewReader("127.0.0.1 my.test"))
	if err != nil {
		t.Fatal(err)
	}
	addr := startProxyWithHosts(t, true, hosts)
	name := []byte("my.test")
	c, code := connect(t, addr, socks5.AtypDomain, append([]byte{byte(len(name))}, name...), port)
	checkEcho(t, c, code)

	// 关闭 IPv4 兼容时，只有 IPv4 地址的 hosts 条目被拒绝
	addr = startProxyWithHosts(t, false, hosts)
	c, code = connect(t, addr, socks5.AtypDomain, append([]byte{byte(len(name))}, name...), port)
	c.Close()
	if code != socks5.ReplyAddrTypeNotSupported {
		t.Errorf("ipv4 off: got %#x", code)
	}
}

func startEcho(echo net.Listener) int {
	go func() {
		c, err := echo.Accept()
		if err != nil {
			return
		}
		io.Copy(c, c) // 读到 EOF 后再关闭，验证半关闭被正确传递
		c.Close()
		echo.Close()
	}()
	return echo.Addr().(*net.TCPAddr).Port
}

func checkEcho(t *testing.T, c net.Conn, code byte) {
	t.Helper()
	defer c.Close()
	if code != socks5.ReplySucceeded {
		t.Fatalf("reply code %#x", code)
	}

	// 空闲超过握手超时，连接不应被断开
	time.Sleep(500 * time.Millisecond)
	payload := bytes.Repeat([]byte("x"), 1<<20)
	go func() {
		c.Write(payload)
		c.(*net.TCPConn).CloseWrite()
	}()
	got, err := io.ReadAll(c)
	if err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("echo mismatch: len=%d err=%v", len(got), err)
	}
}

func TestReplyCodes(t *testing.T) {
	addr := startProxy(t, false)

	c, code := connect(t, addr, socks5.AtypIPv4, []byte{1, 2, 3, 4}, 80)
	c.Close()
	if code != socks5.ReplyAddrTypeNotSupported {
		t.Errorf("IPv4: got %#x", code)
	}

	// 找一个没有监听的端口
	l, err := net.Listen("tcp6", "[::1]:0")
	if err != nil {
		t.Skip("no IPv6 loopback:", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	c, code = connect(t, addr, socks5.AtypIPv6, net.IPv6loopback, port)
	c.Close()
	if code != socks5.ReplyConnectionRefused {
		t.Errorf("refused: got %#x", code)
	}
}
