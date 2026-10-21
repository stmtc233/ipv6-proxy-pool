package netif

import "net"

// Add 以 /128 添加地址，并发执行 netsh
func Add(name string, ips []net.IP, progress func(int)) error {
	return runEach(len(ips), progress, func(i int) error {
		return run("netsh", "interface", "ipv6", "add", "address", name, ips[i].String()+"/128")
	})
}

// Delete 删除地址，并发执行 netsh
func Delete(name string, addrs []*net.IPNet, progress func(int)) error {
	return runEach(len(addrs), progress, func(i int) error {
		return run("netsh", "interface", "ipv6", "delete", "address", name, addrs[i].IP.String())
	})
}
