package main

import (
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"time"
)

var ipv6Addresses []string
var counter *Counter

type Counter struct {
	mu     sync.Mutex
	count  int
	maxVal int
}

func NewCounter(maxVal int) *Counter {
	return &Counter{
		maxVal: maxVal,
	}
}

func (c *Counter) Increment() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.count == c.maxVal {
		c.count = 0
	}

	currentCount := c.count
	c.count++
	return currentCount
}
func isFirstCharacterTwo(input string) bool {
	if len(input) == 0 {
		return false
	}

	firstChar := input[0]
	return firstChar == '2'
}
func getIPv6Addresses() ([]string, error) {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil, err
	}

	var ipv6Addresses []string
	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() == nil && ipnet.IP.To16() != nil {
				if isFirstCharacterTwo(ipnet.IP.String()) {
					ipv6Addresses = append(ipv6Addresses, ipnet.IP.String())
				}
			}
		}
	}
	fmt.Printf("You have %d IPv6 addresses.\n", len(ipv6Addresses))

	return ipv6Addresses, nil
}

func handleClient(clientConn net.Conn) {
	defer clientConn.Close()

	// 接受客户端请求
	buf := make([]byte, 256)
	_, err := clientConn.Read(buf)
	if err != nil {
		fmt.Println("Error reading from client:", err)
		return
	}

	// 解析SOCKS请求
	if buf[0] != 0x05 {
		fmt.Println("Unsupported SOCKS version")
		return
	}

	//numMethods := int(buf[1])
	_, err = clientConn.Write([]byte{0x05, 0x00}) // 告诉客户端我们支持无需认证
	if err != nil {
		fmt.Println("Error writing to client:", err)
		return
	}

	// 解析连接请求
	n, err := clientConn.Read(buf)
	if err != nil {
		fmt.Println("Error reading from client:", err)
		return
	}

	if buf[0] != 0x05 || buf[1] != 0x01 {
		fmt.Println("Unsupported SOCKS request")
		return
	}

	addressType := buf[3]
	var destAddr string

	switch addressType {
	case 0x01: // IPv4地址
		destAddr = net.IP(buf[4:8]).String()
	case 0x03: // 域名
		destAddr = string(buf[5 : n-2]) // 去掉第一个字节（表示域名长度）和最后两个字节（表示端口）
	default:
		fmt.Println("Unsupported address type")
		return
	}

	destPort := int(buf[n-2])<<8 + int(buf[n-1])

	// 建立到目标服务器的连接

	destConn, err := zdipfw("tcp6", fmt.Sprintf("%s:%d", destAddr, destPort), ipv6Addresses[counter.Increment()])

	if err != nil {
		fmt.Println("Error connecting to destination:", err)
		return
	}
	defer destConn.Close()

	// 告诉客户端连接已建立
	_, err = clientConn.Write([]byte{0x05, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00})
	if err != nil {
		fmt.Println("Error writing to client:", err)
		return
	}

	// 转发数据
	go func() {
		_, err := io.Copy(destConn, clientConn)
		if err != nil {
			fmt.Println("Error copying from client to destination:", err)
		}
	}()

	_, err = io.Copy(clientConn, destConn)
	if err != nil {
		fmt.Println("Error copying from destination to client:", err)
	}
}
func zdipfw(netw, addr string, fwip string) (net.Conn, error) {
	//本地地址  ipaddr是本地外网IP
	lAddr, err := net.ResolveTCPAddr(netw, "["+fwip+"]:0")
	if err != nil {
		return nil, err
	}
	//被请求的地址
	rAddr, err := net.ResolveTCPAddr(netw, addr)
	if err != nil {
		return nil, err
	}
	conn, err := net.DialTCP(netw, lAddr, rAddr)
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(35 * time.Second)
	conn.SetDeadline(deadline)
	return conn, nil
}
func main() {
	ipv6Addresses, _ = getIPv6Addresses()
	maxVal := len(ipv6Addresses)
	counter = NewCounter(maxVal)

	listenAddr := "0.0.0.0:1080"
	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		fmt.Println("Error starting proxy:", err)
		os.Exit(1)
	}
	defer listener.Close()

	fmt.Printf("SOCKS proxy is listening on %s...\n", listenAddr)

	for {
		clientConn, err := listener.Accept()
		if err != nil {
			fmt.Println("Error accepting client connection:", err)
			continue
		}
		go handleClient(clientConn)
	}
}
