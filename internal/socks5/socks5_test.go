package socks5

import (
	"bytes"
	"io"
	"net"
	"testing"
)

type result struct {
	req *Request
	err error
}

// handshake 在 net.Pipe 上运行 ReadRequest，客户端发送 msg 并读取服务端的全部回复
func handshake(t *testing.T, requireAuth bool, msg []byte) (*Request, error, []byte) {
	t.Helper()
	client, server := net.Pipe()
	done := make(chan result, 1)
	go func() {
		req, err := ReadRequest(server, requireAuth)
		server.Close()
		done <- result{req, err}
	}()
	go func() {
		client.Write(msg)
	}()
	reply, _ := io.ReadAll(client)
	r := <-done
	return r.req, r.err, reply
}

var connectV6 = append(append([]byte{5, 1, 0, AtypIPv6}, net.ParseIP("2001:db8::1")...), 0, 80)

func userPass(user, pass string) []byte {
	b := []byte{userPassVersion, byte(len(user))}
	b = append(b, user...)
	b = append(b, byte(len(pass)))
	return append(b, pass...)
}

func TestNoAuth(t *testing.T) {
	msg := append([]byte{5, 2, MethodNoAuth, MethodUserPass}, connectV6...)
	req, err, reply := handshake(t, false, msg)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(reply, []byte{5, MethodNoAuth}) {
		t.Errorf("reply %v", reply)
	}
	if req.Address() != "[2001:db8::1]:80" || req.Username != "" {
		t.Errorf("request %+v", req)
	}
}

func TestUserPass(t *testing.T) {
	for _, requireAuth := range []bool{true, false} {
		msg := []byte{5, 1, MethodUserPass}
		msg = append(msg, userPass("alice", "secret")...)
		msg = append(msg, connectV6...)
		req, err, reply := handshake(t, requireAuth, msg)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(reply, []byte{5, MethodUserPass, userPassVersion, 0}) {
			t.Errorf("reply %v", reply)
		}
		if req.Username != "alice" || req.Password != "secret" {
			t.Errorf("credentials %+v", req)
		}
	}
}

func TestRequireAuthRejectsNoAuth(t *testing.T) {
	_, err, reply := handshake(t, true, []byte{5, 1, MethodNoAuth})
	if err != ErrNoAcceptableMethod || !bytes.Equal(reply, []byte{5, MethodNoAcceptable}) {
		t.Errorf("err=%v reply=%v", err, reply)
	}
}

func TestDomainRequest(t *testing.T) {
	msg := []byte{5, 1, MethodNoAuth, 5, 1, 0, AtypDomain, 11}
	msg = append(msg, "example.com"...)
	msg = append(msg, 1, 187)
	req, err, _ := handshake(t, false, msg)
	if err != nil || req.Address() != "example.com:443" {
		t.Errorf("req=%+v err=%v", req, err)
	}
}
