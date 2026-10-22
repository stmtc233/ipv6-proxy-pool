// Command bench 是 ipv6-proxy-pool 的压测工具：通过 SOCKS5 代理连接目标服务，测量
//
//	conn  新建连接速率：每个连接完成握手、往返 1 字节后由目标先关闭（短连接场景）
//	idle  并发连接数：建立 -c 个连接并保持 -d，配合 -pid 计算代理每个连接的内存和 fd 开销
//	bulk  吞吐量：-c 个连接持续从目标下载 -d
//
// 默认在 -target 地址上同时启动内置目标服务。目标放在另一台机器上更接近真实情况：
// 在那台机器运行 bench -mode target -target [::]:9000，本机加 -serve=false 并把 -target 指向它。
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// 连接目标后发送的第一个字节，决定内置目标服务的行为
const (
	cmdOnce     = 'o' // 回显 1 字节后关闭
	cmdHold     = 'h' // 保持连接，直到客户端关闭
	cmdGenerate = 'g' // 持续发送数据
)

var (
	proxyAddr  = flag.String("proxy", "127.0.0.1:1080", "SOCKS5 代理地址")
	targetAddr = flag.String("target", "[::1]:9000", "目标地址（经代理连接）")
	serve      = flag.Bool("serve", true, "在 -target 上启动内置目标服务")
	mode       = flag.String("mode", "conn", "conn | idle | bulk | target")
	conc       = flag.Int("c", 64, "并发数（idle 模式为连接数）")
	duration   = flag.Duration("d", 10*time.Second, "持续时间（idle 模式为保持时间）")
	user       = flag.String("user", "", "SOCKS5 用户名（hash 模式必须）")
	pass       = flag.String("pass", "", "SOCKS5 密码")
	pid        = flag.Int("pid", 0, "代理进程 PID，idle 模式下读取其内存和 fd 数（仅 Linux）")
)

func main() {
	flag.Parse()
	if *mode == "target" || *serve {
		ln, err := net.Listen("tcp", *targetAddr)
		if err != nil {
			fatal(err)
		}
		go serveTarget(ln)
		if *mode == "target" {
			fmt.Println("target listening on", ln.Addr())
			select {}
		}
	}

	switch *mode {
	case "conn":
		runConn()
	case "idle":
		runIdle()
	case "bulk":
		runBulk()
	default:
		fatal(fmt.Errorf("unknown mode %q", *mode))
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}

func serveTarget(ln net.Listener) {
	for {
		c, err := ln.Accept()
		if err != nil {
			time.Sleep(10 * time.Millisecond) // 例如 fd 耗尽，避免空转
			continue
		}
		go func() {
			defer c.Close()
			var cmd [1]byte
			if _, err := io.ReadFull(c, cmd[:]); err != nil {
				return
			}
			switch cmd[0] {
			case cmdOnce:
				if _, err := io.ReadFull(c, cmd[:]); err == nil {
					c.Write(cmd[:])
				}
			case cmdHold:
				io.Copy(io.Discard, c)
			case cmdGenerate:
				buf := make([]byte, 64<<10)
				for {
					if _, err := c.Write(buf); err != nil {
						return
					}
				}
			}
		}()
	}
}

// runConn 测量新建连接速率和延迟
func runConn() {
	var (
		wg    sync.WaitGroup
		mu    sync.Mutex
		lats  []time.Duration
		stats = newErrStats()
	)
	start := time.Now()
	deadline := start.Add(*duration)
	for range *conc {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var local []time.Duration
			buf := []byte{cmdOnce, 'x'}
			for time.Now().Before(deadline) {
				t0 := time.Now()
				c, err := dial(cmdOnce)
				if err == nil {
					_, err = c.Write(buf[1:])
					if err == nil {
						_, err = io.ReadFull(c, buf[1:])
					}
					c.Close()
				}
				if err != nil {
					stats.add(err)
					continue
				}
				local = append(local, time.Since(t0))
			}
			mu.Lock()
			lats = append(lats, local...)
			mu.Unlock()
		}()
	}
	wg.Wait()
	elapsed := time.Since(start)

	slices.Sort(lats)
	fmt.Printf("connections: %d ok in %v, %.0f conn/s\n", len(lats), elapsed.Round(time.Millisecond), float64(len(lats))/elapsed.Seconds())
	if len(lats) > 0 {
		pct := func(p float64) time.Duration { return lats[int(float64(len(lats)-1)*p)] }
		fmt.Printf("latency: p50 %v  p90 %v  p99 %v  max %v\n", pct(0.5), pct(0.9), pct(0.99), lats[len(lats)-1])
	}
	stats.print()
}

// runIdle 建立并保持大量连接，测量代理每个连接的资源开销
func runIdle() {
	before, beforeErr := procStats(*pid)
	var (
		mu     sync.Mutex
		conns  []net.Conn
		stats  = newErrStats()
		wg     sync.WaitGroup
		tokens = make(chan struct{}, 256) // 限制同时进行中的握手数
	)
	start := time.Now()
	for range *conc {
		tokens <- struct{}{}
		wg.Add(1)
		go func() {
			defer func() { <-tokens; wg.Done() }()
			c, err := dial(cmdHold)
			if err != nil {
				stats.add(err)
				return
			}
			mu.Lock()
			conns = append(conns, c)
			mu.Unlock()
		}()
	}
	wg.Wait()
	fmt.Printf("connections: %d open (%v)\n", len(conns), time.Since(start).Round(time.Millisecond))
	stats.print()

	if *pid != 0 {
		time.Sleep(time.Second)
		after, err := procStats(*pid)
		switch {
		case beforeErr != nil:
			fmt.Println("proxy stats:", beforeErr)
		case err != nil:
			fmt.Println("proxy stats:", err)
		default:
			fmt.Printf("proxy: RSS %d -> %d KiB, fds %d -> %d\n", before.rssKiB, after.rssKiB, before.fds, after.fds)
			if n := len(conns); n > 0 {
				fmt.Printf("per connection: %.1f KiB RSS, %.1f fds\n",
					float64(after.rssKiB-before.rssKiB)/float64(n), float64(after.fds-before.fds)/float64(n))
			}
		}
	}

	fmt.Printf("holding for %v\n", *duration)
	time.Sleep(*duration)
	for _, c := range conns {
		c.Close()
	}
}

// runBulk 测量下载吞吐量
func runBulk() {
	var (
		total atomic.Int64
		wg    sync.WaitGroup
		stats = newErrStats()
	)
	start := time.Now()
	deadline := start.Add(*duration)
	for range *conc {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c, err := dial(cmdGenerate)
			if err != nil {
				stats.add(err)
				return
			}
			defer c.Close()
			c.SetReadDeadline(deadline)
			buf := make([]byte, 64<<10)
			for {
				n, err := c.Read(buf)
				total.Add(int64(n))
				if err != nil {
					var ne net.Error
					if !errors.As(err, &ne) || !ne.Timeout() {
						stats.add(err)
					}
					return
				}
			}
		}()
	}
	wg.Wait()
	elapsed := time.Since(start)
	bytes := float64(total.Load())
	fmt.Printf("transferred %.1f MiB in %v: %.2f Gbit/s\n", bytes/(1<<20), elapsed.Round(time.Millisecond), bytes*8/elapsed.Seconds()/1e9)
	stats.print()
}

// dial 经代理连接目标，并发送一个字节的目标命令
func dial(cmd byte) (net.Conn, error) {
	c, err := net.DialTimeout("tcp", *proxyAddr, 10*time.Second)
	if err != nil {
		return nil, err
	}
	c.SetDeadline(time.Now().Add(10 * time.Second))
	if err := handshake(c, *targetAddr); err != nil {
		c.Close()
		return nil, err
	}
	c.SetDeadline(time.Time{})
	if _, err := c.Write([]byte{cmd}); err != nil {
		c.Close()
		return nil, err
	}
	return c, nil
}

type replyError byte

func (e replyError) Error() string { return fmt.Sprintf("socks5 reply %#x", byte(e)) }

// handshake 和常见客户端一样逐步完成 SOCKS5 握手（不把多个报文合并发送）
func handshake(c net.Conn, target string) error {
	method := byte(0x00)
	if *user != "" {
		method = 0x02
	}
	if _, err := c.Write([]byte{5, 1, method}); err != nil {
		return err
	}
	var resp [2]byte
	if _, err := io.ReadFull(c, resp[:]); err != nil {
		return err
	}
	if resp[1] != method {
		return fmt.Errorf("socks5: method %#x rejected", method)
	}
	if method == 0x02 {
		auth := append([]byte{1, byte(len(*user))}, *user...)
		auth = append(append(auth, byte(len(*pass))), *pass...)
		if _, err := c.Write(auth); err != nil {
			return err
		}
		if _, err := io.ReadFull(c, resp[:]); err != nil {
			return err
		}
	}

	host, portStr, err := net.SplitHostPort(target)
	if err != nil {
		return err
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return err
	}
	req := []byte{5, 1, 0}
	if ip := net.ParseIP(host); ip == nil {
		req = append(append(req, 3, byte(len(host))), host...)
	} else if ip4 := ip.To4(); ip4 != nil {
		req = append(append(req, 1), ip4...)
	} else {
		req = append(append(req, 4), ip.To16()...)
	}
	req = append(req, byte(port>>8), byte(port))
	if _, err := c.Write(req); err != nil {
		return err
	}

	var hdr [4]byte
	if _, err := io.ReadFull(c, hdr[:]); err != nil {
		return err
	}
	if hdr[1] != 0 {
		return replyError(hdr[1])
	}
	rest := 4 + 2
	if hdr[3] == 4 {
		rest = 16 + 2
	}
	_, err = io.ReadFull(c, make([]byte, rest))
	return err
}

// errStats 按错误类别计数，避免地址、端口不同的同类错误刷屏
type errStats struct {
	mu sync.Mutex
	m  map[string]int
}

func newErrStats() *errStats { return &errStats{m: map[string]int{}} }

func (s *errStats) add(err error) {
	var (
		re    replyError
		ne    net.Error
		errno syscall.Errno
		kind  string
	)
	switch {
	case errors.As(err, &re):
		kind = re.Error()
	case errors.As(err, &ne) && ne.Timeout():
		kind = "timeout"
	case errors.As(err, &errno):
		kind = errno.Error()
	case errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF):
		kind = "EOF"
	default:
		kind = err.Error()
	}
	s.mu.Lock()
	s.m[kind]++
	s.mu.Unlock()
}

func (s *errStats) print() {
	for kind, n := range s.m {
		fmt.Printf("errors: %d × %s\n", n, kind)
	}
}

type stats struct {
	rssKiB int
	fds    int
}

// procStats 读取进程的常驻内存和打开的 fd 数（Linux /proc）
func procStats(pid int) (stats, error) {
	var st stats
	if pid == 0 {
		return st, errors.New("no -pid")
	}
	status, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		return st, err
	}
	for line := range strings.Lines(string(status)) {
		if v, ok := strings.CutPrefix(line, "VmRSS:"); ok {
			st.rssKiB, _ = strconv.Atoi(strings.TrimSuffix(strings.TrimSpace(v), " kB"))
		}
	}
	fds, err := os.ReadDir(fmt.Sprintf("/proc/%d/fd", pid))
	if err != nil {
		return st, err
	}
	st.fds = len(fds)
	return st, nil
}
