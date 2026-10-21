# ipv6-proxy-pool

一个只出 IPv6 的 SOCKS5 代理：每个连接从地址池中选择一个 IPv6 地址作为出口，可以顺序轮换、随机，或按用户名/密码锁定 IP。

支持 Linux 和 Windows（`anyipv6` 模式仅 Linux）。仅供学习参考。

![演示](ys.gif)

## 特性

- 两种地址来源
  - `nic`：在网卡前缀内批量生成随机地址并添加到网卡
  - `anyipv6`：不添加地址，直接使用整个前缀（freebind），地址池等于整个前缀
- 三种选择方式：`sequential` 顺序、`random` 随机、`hash` 按凭据锁定 IP
- 支持任意前缀长度（/48、/56、/64、/112……），只要小于 128
- SOCKS5 无认证 / 用户名密码认证（RFC 1929）
- 只在握手阶段设超时，长连接不会被断开；目标拨号有超时，域名解析结果有缓存
- Linux 下转发走 splice 零拷贝；批量增删地址使用 `ip -batch`

## 环境要求

- 网卡上至少有一个公网 IPv6 地址，且前缀长度小于 128（如 `2001:db8::1/64`）
- 修改网卡地址或路由需要 root / 管理员权限
- Linux 推荐安装 iproute2（`ip` 命令）；没有时 `nic` 模式会退回逐个调用 `ifconfig`
- 只支持 IPv6 目标：IPv4 地址和只有 A 记录的域名会被拒绝

## 快速开始

1. 从 [Releases](https://github.com/stmtc233/ipv6-proxy-pool/releases) 下载对应平台的程序，或自行编译：
   ```bash
   go build -o ipv6proxypool .
   ```
2. 编辑与程序**同一目录**下的 `config.ini`，至少改好网卡名 `Networkname`
3. 以 root / 管理员身份运行，按提示操作：
   ```bash
   sudo ./ipv6proxypool
   ```
4. 测试：
   ```bash
   curl -x socks5h://127.0.0.1:1080 https://api64.ipify.org
   ```
   多请求几次，`nic` / `anyipv6` 配合 `sequential` 或 `random` 时每次出口 IP 都不同。

## 配置

```ini
Networkname = eth0    ; 网卡名（Windows 填网络连接名称，如 以太网）
port = 1080           ; 监听端口，绑定 0.0.0.0

mode = nic            ; 地址来源：nic | anyipv6
select = sequential   ; 选择方式：sequential | random | hash
prefix =              ; anyipv6 使用的前缀，如 2001:db8::/48；留空则从网卡获取
```

`mode`、`select`、`prefix` 不写时使用默认值。`anyipv6` 模式下填写了 `prefix` 时可以不填 `Networkname`。

## 地址来源 `mode`

### `nic`（默认）

启动时依次询问：

1. **是否删除前缀地址以外的地址**：删除网卡上之前添加的 /128 地址，保留前缀长度小于 128 的原有地址
2. **是否添加地址以及数量**：在第一个前缀地址所在的前缀内生成不重复的随机地址，以 /128 添加到网卡。前缀太小时（如 /120 只有 256 个）只添加剩余可用的数量

之后网卡上所有公网 IPv6 地址组成出口池。

> __删除操作会移除网卡上的 /128 公网地址，执行前请确认没有其他用途。__

### `anyipv6`（仅 Linux）

不往网卡添加任何地址，每次连接直接绑定前缀内的任意地址作为出口，整个前缀都是地址池。

- 前缀优先使用 `prefix`，留空则取网卡上第一个前缀长度小于 128 的地址所在前缀
- 启动时可选择执行 `ip -6 route replace local <前缀> dev lo`，没有这条路由回包无法被本机接收
- **上游必须把这个前缀路由到本机**。如果前缀是 on-link 的（上游通过邻居发现逐个找地址，常见于只给一个 /64 的 VPS），还需要 [ndppd](https://github.com/DanielAdolfsson/ndppd) 之类的 NDP 代理
- 如果网关地址就在这个前缀内，添加 local 路由会导致网络不通。这种情况请改用 `nic` 模式，或者用 `prefix` 指定一个不包含网关的更小子网

## 选择方式 `select`

| 值 | 说明 |
| --- | --- |
| `sequential`（默认） | 按顺序轮流使用 |
| `random` | 每个连接随机选择 |
| `hash` | 根据 SOCKS5 用户名和密码计算出口，相同凭据总是同一个 IP |

`hash` 说明：

- 客户端**必须**使用用户名/密码认证，凭据不做校验，可以任意填写，例如用不同用户名区分会话：
  ```bash
  curl -x socks5h://user1:x@127.0.0.1:1080 https://api64.ipify.org   # 每次都是同一个 IP
  curl -x socks5h://user2:x@127.0.0.1:1080 https://api64.ipify.org   # 另一个固定 IP
  ```
- `anyipv6` 模式下只要前缀不变，对应关系就固定；`nic` 模式下网卡地址数量变化后对应关系会改变

## 注意

- 代理没有访问控制，监听在 0.0.0.0，请用防火墙限制来源，避免成为公开代理
- 只支持 TCP CONNECT，不支持 UDP ASSOCIATE 和 BIND
