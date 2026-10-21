package netif

import (
	"bytes"
	"fmt"
	"net"
	"os/exec"
	"strings"
)

// ip -batch 每批的命令数，只影响进度条刷新粒度
const ipBatchSize = 256

// Add 以 /128 添加地址。有 iproute2 时用 ip -batch 批量执行，否则逐个调用 ifconfig。
func Add(name string, ips []net.IP, progress func(int)) error {
	if hasIP() {
		lines := make([]string, len(ips))
		for i, ip := range ips {
			// nodad：跳过重复地址检测，地址立即可用于 bind，不会因 tentative 状态拨号失败
			lines[i] = fmt.Sprintf("address add %s/128 dev %s nodad", ip, name)
		}
		return ipBatch(lines, progress)
	}
	return runEach(len(ips), progress, func(i int) error {
		return run("ifconfig", name, "inet6", "add", ips[i].String()+"/128")
	})
}

// Delete 删除地址。内核按地址和前缀长度匹配，因此带上原有的前缀长度。
func Delete(name string, addrs []*net.IPNet, progress func(int)) error {
	if hasIP() {
		lines := make([]string, len(addrs))
		for i, a := range addrs {
			lines[i] = fmt.Sprintf("address del %s dev %s", a, name)
		}
		return ipBatch(lines, progress)
	}
	return runEach(len(addrs), progress, func(i int) error {
		return run("ifconfig", name, "inet6", "del", addrs[i].String())
	})
}

func hasIP() bool {
	_, err := exec.LookPath("ip")
	return err == nil
}

func ipBatch(lines []string, progress func(int)) error {
	var (
		failed int
		first  error
	)
	for start := 0; start < len(lines); start += ipBatchSize {
		end := min(start+ipBatchSize, len(lines))
		cmd := exec.Command("ip", "-6", "-force", "-batch", "-")
		cmd.Stdin = strings.NewReader(strings.Join(lines[start:end], "\n") + "\n")
		if out, err := cmd.CombinedOutput(); err != nil {
			if failed++; first == nil {
				first = fmt.Errorf("ip -batch: %v: %s", err, bytes.TrimSpace(out))
			}
		}
		progress(end - start)
	}
	if failed > 0 {
		return fmt.Errorf("%d batch(es) reported errors, first error: %v", failed, first)
	}
	return nil
}
