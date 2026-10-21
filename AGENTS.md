# AGENTS.md

本文件给在本仓库中工作的 AI 编程助手提供上下文。

## 项目概述

只出 IPv6 的 SOCKS5 代理（Go）。每个连接从地址池中选择一个本地 IPv6 地址作为出口。
单个可执行文件，启动时读取工作目录下的 `config.ini`，通过交互式提示完成网卡地址/路由设置后开始监听。

## 目录结构

```
main.go                 启动流程：读配置、交互提示（stdin）、按 mode 构建 Source、启动服务
internal/socks5         SOCKS5 协议：认证协商（无认证 / RFC 1929 用户名密码）、请求解析、应答
internal/pool           出口地址池：Source / Selector 接口及实现、前缀地址计算
internal/proxy          服务端：Accept 循环、拨号、双向转发、AAAA 缓存
internal/netif          网卡操作：读取地址、增删地址、freebind、local 路由（按平台分文件）
config.ini              默认配置，随 Release 一起发布
hosts                   自定义域名解析示例（全部为注释），随 Release 一起发布
.github/workflows       Release 时用 go-release-action 交叉编译 linux/amd64、linux/arm64、windows/amd64
```

## 核心设计：Source × Selector

地址池拆成两个**正交**维度，新增任何一种实现都不应修改另一维度：

- `pool.Selector`：根据 `pool.Client`（用户名、密码、客户端地址）产出 128 位 `pool.Key`
  - `Sequential`（原子计数）、`Random`（math/rand/v2）、`Hash`（SHA-256(用户名 \x00 密码)）
- `pool.Source`：把 `Key` 映射成本地地址 `*net.TCPAddr`
  - `NIC`：`addrs[Key.Lo % len]`，地址在启动时预解析
  - `AnyIP`：用 `Key` 填充前缀主机位（`AddrInPrefix`），支持任意前缀长度
- 需要在拨号前设置 socket 选项的 Source 实现 `pool.Controller`（`AnyIP` 用它设置 freebind），
  `proxy.Server` 会自动挂到 `net.Dialer.Control`
- 目标解析顺序（`Server.resolve`）：IP 字面量 > hosts 文件（`proxy.Hosts`，每次返回打乱顺序的副本）> DNS（`proxy.Resolver`，带缓存）
- IPv4 兼容（`Server.AllowIPv4`）不经过地址池：IPv4 目标用 `tcp4` 走系统默认出口；有 AAAA 记录的域名始终走 IPv6
- 新增模式时：在 `pool` 中实现接口，在 `pool.NewSelector` 或 `main.go` 的 `mode` 分支注册，
  同步更新 `config.ini` 注释和 `README.md`

## 开发命令

```bash
go build ./...
go vet ./...
gofmt -l .                        # 应无输出
go test -race ./...
# 提交前确认所有发布平台都能编译
for t in linux/amd64 linux/arm64 windows/amd64; do GOOS=${t%/*} GOARCH=${t#*/} go vet ./... && GOOS=${t%/*} GOARCH=${t#*/} go build -o /dev/null .; done
```

Go 版本见 `go.mod`；修改 Go 版本时同步修改 `.github/workflows/build.yml` 中的 `goversion`。

## 约定

- 代码注释、提示文本中的说明使用中文；程序输出的提示/日志沿用英文
- 平台相关代码用文件名后缀或 `//go:build` 分文件（`netif_linux.go`、`netif_windows.go`、`netif_other.go`、`anyip_notlinux.go`），
  每个导出函数在所有平台都要有实现，保证交叉编译通过
- 调用外部命令时直接 `exec.Command(name, args...)`，不要经过 shell 拼接
- 依赖保持精简，新增依赖前先确认标准库或 `golang.org/x/sys` 无法满足

## 性能相关的注意事项（热路径在 internal/proxy）

- 转发两端必须保持为 `*net.TCPConn` 直接 `io.Copy`，Linux 上才会走 splice 零拷贝；不要包装连接（计数、限速、空闲超时包装都会破坏它）
- 只在握手阶段设置 deadline，握手完成后清除；不要给已建立的连接设绝对 deadline，死连接依靠 TCP keepalive 发现
- 每个连接的路径上不要加锁、不要做重复解析、不要输出正常关闭类的日志（见 `logErr`）
- 一个方向读到 EOF 时只 `CloseWrite` 半关闭；出错时关闭两端

## 测试

- `internal/pool`、`internal/socks5` 的单元测试不依赖网络
- `internal/proxy/server_test.go` 需要 IPv6 回环；没有 IPv6 的环境（很多容器/CI）会自动 Skip，不代表通过
- 增删网卡地址、local 路由、anyipv6 实际转发需要 root 和真实 IPv6 网络，无法在 CI 中验证；改动这部分时在说明中注明未经实机验证的内容

## 安全

- 程序会修改网卡地址和路由，涉及删除的逻辑改动需格外谨慎（只删除 /128 的非前缀地址）
- 代理本身没有访问控制，`hash` 模式下凭据也不做校验
