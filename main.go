package main

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"runtime"
	"slices"
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
	// 监听地址：只写主机（如 0.0.0.0、::、127.0.0.1）时与 port 组合；也可直接写 host:port
	listen := section.Key("listen").MustString("0.0.0.0")
	listenAddr := listen
	if _, _, err := net.SplitHostPort(listen); err != nil {
		if port == "" {
			errhandling(fmt.Errorf("check config.ini: port is required when listen has no port"))
		}
		listenAddr = net.JoinHostPort(listen, port)
	}
	// IPv4 兼容：off 拒绝 IPv4 目标（默认）；on 允许，走系统默认 IPv4 出口
	ipv4 := section.Key("ipv4").MustString("off")
	if ipv4 != "off" && ipv4 != "on" {
		errhandling(fmt.Errorf("check config.ini: ipv4 must be off or on, got %q", ipv4))
	}
	// 地址来源 mode = nic | anyipv6，选择策略 select = sequential | random | hash
	mode := section.Key("mode").MustString(pool.ModeNIC)
	selectName := section.Key("select").MustString(pool.SelectSequential)
	prefixStr := section.Key("prefix").String() // anyipv6 使用的前缀，留空则从网卡获取
	// 只有 anyipv6 且自定义了前缀时才不需要网卡名
	if networkName == "" && (mode != pool.ModeAnyIPv6 || prefixStr == "") {
		errhandling(fmt.Errorf("check config.ini: Networkname is required"))
	}
	fmt.Println("Networkname:", networkName)
	fmt.Println("Mode:", mode, "Select:", selectName, "IPv4:", ipv4)

	selector, err := pool.NewSelector(selectName)
	if err != nil {
		errhandling(err)
	}

	var source pool.Source
	switch mode {
	case pool.ModeNIC:
		source, err = setupNIC(networkName)
	case pool.ModeAnyIPv6:
		source, err = setupAnyIP(networkName, prefixStr)
	default:
		err = fmt.Errorf("unknown mode %q", mode)
	}
	if err != nil {
		errhandling(err)
	}

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
		RequireAuth:      selectName == pool.SelectHash,
		AllowIPv4:        ipv4 == "on",
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
	for _, a := range addrs {
		fmt.Println("IPv6 地址:", a)
	}
	// 前缀长度小于 128 的地址视为网卡原有的前缀地址，新地址在其前缀内生成
	bases := prefixAddrs(addrs)
	if len(bases) == 0 {
		return nil, fmt.Errorf("no IPv6 address with prefix length < 128 on %s", networkName)
	}

	// 删除除了前缀地址之外的ipv6地址（即之前添加的 /128 地址）
	if promptForYesNo("Remove addresses other than prefix addresses(!!!)") {
		var del []*net.IPNet
		for _, a := range addrs {
			if !slices.Contains(bases, a) {
				del = append(del, a)
			}
		}
		fmt.Println("Removing")
		runWithProgress(len(del), func(progress func(int)) error {
			return netif.Delete(networkName, del, progress)
		})
		fmt.Println("Remove completed")
		addrs = bases
	}

	if promptForYesNo("Add ipv6 address") {
		prefix := pool.PrefixOf(bases[0])
		fmt.Printf("Prefix: %s (%d host bits)\n", prefix, pool.HostBits(prefix))
		n := promptForInt("Add quantity:")
		existing := make([]net.IP, len(addrs))
		for i, a := range addrs {
			existing[i] = a.IP
		}
		ips := pool.RandomInPrefix(prefix, n, existing)
		if len(ips) < n {
			fmt.Printf("Prefix %s only has room for %d more addresses\n", prefix, len(ips))
		}
		fmt.Println("Adding")
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

// setupAnyIP 确定前缀（配置优先，否则取网卡上的前缀），并按提示添加 local 路由
func setupAnyIP(networkName, prefixStr string) (*pool.AnyIP, error) {
	if runtime.GOOS != "linux" {
		return nil, fmt.Errorf("anyipv6 mode only supports Linux")
	}

	var prefix *net.IPNet
	if prefixStr != "" {
		var err error
		if prefix, err = pool.ParsePrefix(prefixStr); err != nil {
			return nil, err
		}
	} else {
		addrs, err := netif.GlobalIPv6(networkName)
		if err != nil {
			return nil, err
		}
		bases := prefixAddrs(addrs)
		if len(bases) == 0 {
			return nil, fmt.Errorf("no IPv6 prefix found on %s, set prefix in config.ini", networkName)
		}
		prefix = pool.PrefixOf(bases[0])
	}
	fmt.Printf("AnyIPv6 prefix: %s (%d host bits)\n", prefix, pool.HostBits(prefix))

	if promptForYesNo(fmt.Sprintf("Add local route (ip -6 route replace local %s dev lo)", prefix)) {
		if err := netif.AddLocalRoute(prefix); err != nil {
			fmt.Println("Failed to run command:", err)
		} else {
			fmt.Println("Route added")
		}
	}
	fmt.Println("Note: the prefix must be routed to this host by upstream. If it is on-link " +
		"(upstream uses neighbor discovery), you also need an NDP proxy such as ndppd. " +
		"If your gateway address is inside this prefix, the local route will break routing.")
	return pool.NewAnyIP(prefix, netif.FreeBind), nil
}

// prefixAddrs 返回前缀长度小于 128 的地址
func prefixAddrs(addrs []*net.IPNet) []*net.IPNet {
	var r []*net.IPNet
	for _, a := range addrs {
		if ones, _ := a.Mask.Size(); ones < 128 {
			r = append(r, a)
		}
	}
	return r
}

func runWithProgress(total int, fn func(progress func(int)) error) {
	bar := pb.StartNew(total)
	err := fn(func(n int) { bar.Add(n) })
	bar.Finish()
	if err != nil {
		fmt.Println("Failed to run command:", err)
	}
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
