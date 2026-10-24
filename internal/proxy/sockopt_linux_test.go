package proxy

import (
	"net"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"

	"gosdl/internal/pool"
)

// 出口拨号的 socket 在 bind 之前应设置 IP_BIND_ADDRESS_NO_PORT，同时仍调用 Source 的 Control
func TestControlBindNoPort(t *testing.T) {
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	srv := newTestServer(t, false)
	srcCalled := false
	srv.Source = controlSource{srv.Source, func() { srcCalled = true }}
	var opt int
	d := net.Dialer{
		LocalAddr: &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)},
		Control: func(network, address string, c syscall.RawConn) error {
			err := srv.control(network, address, c)
			c.Control(func(fd uintptr) {
				opt, _ = unix.GetsockoptInt(int(fd), unix.IPPROTO_IP, unix.IP_BIND_ADDRESS_NO_PORT)
			})
			return err
		},
	}
	c, err := d.Dial("tcp4", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	c.Close()
	if opt != 1 {
		t.Errorf("IP_BIND_ADDRESS_NO_PORT = %d", opt)
	}
	if !srcCalled {
		t.Error("source Control was not called")
	}
}

// controlSource 记录 Control 是否被调用
type controlSource struct {
	pool.Source
	called func()
}

func (s controlSource) Control(network, address string, c syscall.RawConn) error {
	s.called()
	return nil
}
