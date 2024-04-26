package main

import (
	"bufio"
	"crypto/rand"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/cheggaaa/pb/v3"
	"gopkg.in/ini.v1"
)

var ipv6Addresses []string
var counter *Counter
var osName string

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

// 获取所有ipv6地址
func getIPv6Addresses(Networkname string) ([]string, error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}

	var ipv6Addresses []string
	for _, iface := range interfaces {
		if iface.Name == Networkname {
			addrs, err := iface.Addrs()
			if err != nil {
				return nil, err
			}

			for _, addr := range addrs {
				if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
					if ipnet.IP.To4() == nil && ipnet.IP.To16() != nil {
						if isFirstCharacterTwo(ipnet.IP.String()) {
							ipv6Addresses = append(ipv6Addresses, ipnet.IP.String())
						}
					}
				}
			}
		}
	}

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
		// destAddr = net.IP(buf[4:8]).String()
		return
	case 0x03: // 域名
		destAddr = string(buf[5 : n-2]) // 去掉第一个字节（表示域名长度）和最后两个字节（表示端口）
	case 0x04: // ipv6地址 没测试行不行应该问题不大
		destAddr = net.IP(buf[4:20]).String()
	default:
		fmt.Println("Unsupported address type")
		return
	}

	destPort := int(buf[n-2])<<8 + int(buf[n-1])

	// 建立到目标服务器的连接

	destConn, err := zdipfw("tcp6", fmt.Sprintf("[%s]:%d", destAddr, destPort), ipv6Addresses[counter.Increment()])

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
	//本地地址
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
	osName = runtime.GOOS

	// 判断操作系统类型
	switch osName {
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

	// 获取Networkname字段
	section := cfg.Section("")
	networkName := section.Key("Networkname").String()
	port := section.Key("port").String()
	if networkName == "" || port == "" {
		fmt.Println("NetworkName:" + networkName)
		fmt.Println("Port:" + port)
		errhandling(fmt.Errorf("check config.ini"))
	}
	fmt.Println("Networkname:", networkName)
	//获取前缀长度为64的公网地址
	ya, err := get64(networkName)
	if err != nil {
		errhandling(err)
	}
	// 获取当前的ipv6地址
	ipv6Addresses, _ = getIPv6Addresses(networkName)
	maxVal := len(ipv6Addresses)
	counter = NewCounter(maxVal)
	// 删除除了ya之外的ipv6地址
	p := promptForYesNo("Remove addresses other than 64 prefix(!!!)")
	if p {
		fmt.Println("Removing")
		processIPv6Addresses(ipv6Addresses, networkName, ya)
		fmt.Println("Remove completed")
	}
	p = promptForYesNo("Add ipv6 address")
	if p {
		//生成地址
		var userInput int
		fmt.Print("Add quantity:")
		fmt.Scanf("%d", &userInput)
		fmt.Println("Adding")
		na := generateRandomIPv6Batch(ya[0], userInput)
		progress := pb.StartNew(len(na))
		for c := 0; c < len(na); c++ {
			setaddres("add", networkName, na[c])
			progress.Increment()
		}
		progress.Finish()
		fmt.Println("Add completed")
	}

	//获取当前地址
	ipv6Addresses, _ = getIPv6Addresses(networkName)
	maxVal = len(ipv6Addresses)
	counter = NewCounter(maxVal)
	fmt.Printf("You have %d IPv6 addresses.\n", len(ipv6Addresses))

	listenAddr := "0.0.0.0:" + port
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
func promptForYesNo(prompt string) bool {
	reader := bufio.NewReader(os.Stdin)

	for {
		fmt.Print(prompt + " (y/n): ")
		input, err := reader.ReadString('\n')
		if err != nil {
			fmt.Println("Error reading input:", err)
			return false
		}

		// 清除输入中的空白字符
		input = strings.TrimSpace(input)

		// 判断输入是否为y或n，不区分大小写
		if strings.EqualFold(input, "y") {
			return true
		} else if strings.EqualFold(input, "n") {
			return false
		}

		fmt.Println("Please enter only 'y' or 'n'")
	}
}
func get64(Networkname string) ([]string, error) {
	// 获取指定网络接口
	iface, err := net.InterfaceByName(Networkname)
	if err != nil {
		return nil, fmt.Errorf("Failed to obtain network interface:" + err.Error())
	}

	// 获取接口的地址信息
	addrs, err := iface.Addrs()
	if err != nil {
		return nil, fmt.Errorf("Failed to obtain address information:" + err.Error())
	}

	// 遍历每个地址
	var r []string
	for _, addr := range addrs {

		addrStr := addr.String()
		// 检查地址字符串中的前缀长度是否为64
		if isFirstCharacterTwo(addrStr) {
			fmt.Println("IPv6 地址:", addr)
			if strings.HasSuffix(addrStr, "/64") {
				r = append(r, strings.TrimSuffix(addrStr, "/64"))
			}
		}

	}
	if len(r) > 0 {
		return r, nil
	} else {
		return nil, fmt.Errorf("no have 64 prefix ipv6 address")
	}

}

// 运行cmd命令
func runCmd(command string) error {
	switch osName {
	case "windows":
		cmd := exec.Command("cmd", "/c", command)
		output, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("failed to run command: %v\noutput: %s", err, output)
		}
		return nil
	case "linux":
		// 使用Command函数创建Cmd结构体
		command := exec.Command("bash", "-c", command)

		// 执行命令并获取输出
		output, err := command.CombinedOutput()
		if err != nil {
			return fmt.Errorf("failed to run command: %v\noutput: %s", err, output)
		}
	}
	return nil
}

func setaddres(set, networkName, ipv6Address string) {
	// 构建netsh命令
	var cmd string
	switch osName {
	case "windows":
		if set == "add" {
			cmd = fmt.Sprintf(`netsh interface ipv6 %s address "%s" %s/128`, set, networkName, ipv6Address)
		} else {
			cmd = fmt.Sprintf(`netsh interface ipv6 %s address "%s" %s`, set, networkName, ipv6Address)
		}
	case "linux":
		if set == "add" {
			cmd = fmt.Sprintf(`ifconfig %s inet6 %s %s/128`, networkName, set, ipv6Address)
		} else {
			cmd = fmt.Sprintf(`ifconfig %s inet6 %s %s`, networkName, set, ipv6Address)
		}
	}

	// 运行命令
	err := runCmd(cmd)
	if err != nil {
		fmt.Println("Failed to run command:", err)
		return
	}
}

// 处理IPv6地址切片
func processIPv6Addresses(ipv6Addresses []string, Networkname string, ya []string) {
	// 遍历IPv6地址切片
	progress := pb.StartNew(len(ipv6Addresses))
	for _, address := range ipv6Addresses {
		// 检查是否包含在ya切片中
		found := false
		for _, prefix := range ya {
			if strings.Contains(address, prefix) {
				found = true
				break
			}
		}
		// 如果包含在ya切片中，则跳过
		if found {
			progress.Increment()
			continue
		}

		// 否则，执行操作
		setaddres("del", Networkname, address)
		progress.Increment()
	}
	progress.Finish()
}

// 生成具有相同64位前缀的随机IPv6地址
func generateRandomIPv6Batch(baseIPv6 string, count int) []string {
	// 解析基础IPv6地址
	baseIP := net.ParseIP(baseIPv6)
	if baseIP == nil {
		return nil
	}

	// 获取前64位前缀
	prefix := baseIP[:8]

	// 生成随机的后64位
	randomIPv6Addresses := make([]string, count)

	for i := 0; i < count; i++ {
		randomSuffix := make([]byte, 8)
		rand.Read(randomSuffix)

		// 合并前64位前缀和随机的后64位
		randomIPv6 := net.IP(append(prefix, randomSuffix...)).String()
		randomIPv6Addresses[i] = randomIPv6
	}

	return randomIPv6Addresses
}
func errhandling(err error) {
	fmt.Println(err.Error())
	fmt.Printf("Press any key to exit...")
	b := make([]byte, 1)
	os.Stdin.Read(b)
	os.Exit(1)
}
