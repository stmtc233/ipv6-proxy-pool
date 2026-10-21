package main

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/cheggaaa/pb/v3"
	"gopkg.in/ini.v1"

	"gosdl/internal/netif"
	"gosdl/internal/pool"
	"gosdl/internal/proxy"
)

const (
	handshakeTimeout = 10 * time.Second
	dialTimeout      = 10 * time.Second
	keepAlive        = 30 * time.Second
	dnsTTL           = 60 * time.Second
	dnsMaxEntries    = 10000
)

// 整个程序共用一个 stdin reader，避免多个缓冲 reader 互相吞掉输入
var stdin = bufio.NewReader(os.Stdin)

func main() {
	// 判断操作系统类型
	switch runtime.GOOS {
	case "windows":
		fmt.Println("System is Windows")
	case "linux":
		fmt.Println("System is Linux")
	default:
		errhandling(fmt.Errorf("unknown system"))
	}

	// 加载INI配置文件
	cfg, err := ini.Load("config.ini")
	if err != nil {
		errhandling(err)
	}
	section := cfg.Section("")
	networkName := section.Key("Networkname").String()
	port := section.Key("port").String()
	if networkName == "" || port == "" {
		fmt.Println("NetworkName:" + networkName)
		fmt.Println("Port:" + port)
		errhandling(fmt.Errorf("check config.ini"))
	}
	// 预留：地址来源 mode = nic | anyipv6，选择策略 select = sequential | random | hash
	mode := section.Key("mode").MustString(pool.ModeNIC)
	selectName := section.Key("select").MustString(pool.SelectSequential)
	fmt.Println("Networkname:", networkName)

	selector, err := pool.NewSelector(selectName)
	if err != nil {
		errhandling(err)
	}

	var source pool.Source
	switch mode {
	case pool.ModeNIC:
		source, err = setupNIC(networkName)
	case pool.ModeAnyIPv6:
		err = fmt.Errorf("mode %q is not implemented yet", mode)
	default:
		err = fmt.Errorf("unknown mode %q", mode)
	}
	if err != nil {
		errhandling(err)
	}

	listenAddr := "0.0.0.0:" + port
	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		fmt.Println("Error starting proxy:", err)
		os.Exit(1)
	}
	defer listener.Close()

	fmt.Printf("SOCKS proxy is listening on %s...\n", listenAddr)

	srv := &proxy.Server{
		Source:           source,
		Selector:         selector,
		Resolver:         proxy.NewResolver(dnsTTL, dnsMaxEntries),
		HandshakeTimeout: handshakeTimeout,
		DialTimeout:      dialTimeout,
		KeepAlive:        keepAlive,
	}
	if err := srv.Serve(listener); err != nil {
		fmt.Println("Proxy stopped:", err)
	}
}

// setupNIC 按提示删除/添加网卡地址，然后以网卡上的公网地址作为出口池
func setupNIC(networkName string) (*pool.NIC, error) {
	addrs, err := netif.GlobalIPv6(networkName)
	if err != nil {
		return nil, err
	}
	// 获取前缀长度为64的公网地址
	var bases []net.IP
	for _, a := range addrs {
		fmt.Println("IPv6 地址:", a)
		if ones, _ := a.Mask.Size(); ones == 64 {
			bases = append(bases, a.IP)
		}
	}
	if len(bases) == 0 {
		return nil, fmt.Errorf("no have 64 prefix ipv6 address")
	}

	// 删除除了64前缀地址之外的ipv6地址
	if promptForYesNo("Remove addresses other than 64 prefix(!!!)") {
		var del []*net.IPNet
		for _, a := range addrs {
			if !containsIP(bases, a.IP) {
				del = append(del, a)
			}
		}
		fmt.Println("Removing")
		runWithProgress(len(del), func(progress func(int)) error {
			return netif.Delete(networkName, del, progress)
		})
		fmt.Println("Remove completed")
	}

	if promptForYesNo("Add ipv6 address") {
		n := promptForInt("Add quantity:")
		fmt.Println("Adding")
		ips := netif.GenerateInPrefix64(bases[0], n)
		runWithProgress(len(ips), func(progress func(int)) error {
			return netif.Add(networkName, ips, progress)
		})
		fmt.Println("Add completed")
	}

	// 获取当前地址
	addrs, err = netif.GlobalIPv6(networkName)
	if err != nil {
		return nil, err
	}
	ips := make([]net.IP, len(addrs))
	for i, a := range addrs {
		ips[i] = a.IP
	}
	fmt.Printf("You have %d IPv6 addresses.\n", len(ips))
	return pool.NewNIC(ips)
}

func runWithProgress(total int, fn func(progress func(int)) error) {
	bar := pb.StartNew(total)
	err := fn(func(n int) { bar.Add(n) })
	bar.Finish()
	if err != nil {
		fmt.Println("Failed to run command:", err)
	}
}

func containsIP(list []net.IP, ip net.IP) bool {
	for _, x := range list {
		if x.Equal(ip) {
			return true
		}
	}
	return false
}

func readLine() (string, error) {
	s, err := stdin.ReadString('\n')
	return strings.TrimSpace(s), err
}

func promptForYesNo(prompt string) bool {
	for {
		fmt.Print(prompt + " (y/n): ")
		input, err := readLine()
		if err != nil && input == "" {
			fmt.Println("Error reading input:", err)
			return false
		}
		// 判断输入是否为y或n，不区分大小写
		if strings.EqualFold(input, "y") {
			return true
		} else if strings.EqualFold(input, "n") {
			return false
		}
		fmt.Println("Please enter only 'y' or 'n'")
	}
}

func promptForInt(prompt string) int {
	for {
		fmt.Print(prompt)
		input, err := readLine()
		if n, convErr := strconv.Atoi(input); convErr == nil && n >= 0 {
			return n
		}
		if err != nil {
			fmt.Println("Error reading input:", err)
			return 0
		}
		fmt.Println("Please enter a non-negative integer")
	}
}

func errhandling(err error) {
	fmt.Println(err.Error())
	fmt.Printf("Press any key to exit...")
	stdin.ReadByte()
	os.Exit(1)
}
