# tailnetSDK

把 [tailscale/tailscale](https://github.com/tailscale/tailscale) 的 **tailnet** 能力封装成可直接嵌入 App 的 SDK。

- 目标是 **Android / iOS / Windows / macOS** 四端
- 隧道**只在 App 进程内**建立：基于官方 `tailscale.com/tsnet`（gVisor 用户态协议栈），**零特权、无需安装 Tailscale 客户端、无需系统 VPN 接口、无需 wintun/NE entitlement**
- 控制面使用 **Tailscale 官方**（`https://controlplane.tailscale.com`，可通过 `Config.ControlURL` 覆写）

## 能力矩阵（对需求逐条落地）

| SDK 能力 | 实现 | 状态 |
|---|---|---|
| 登录授权 | `Node.StartLoginInteractive` / `Node.LoginURL`（浏览器打开 URL，节点通过控制连接自动感知授权完成，**无需回调 URI**） | ✅ M0 |
| 授权状态查询 | `Node.Status` / `Node.StatusWithoutPeers` / `Node.Watch`（事件流） | ✅ M0 |
| 注销 | `Node.Logout`，另有 `ProfileStatus` / `SwitchProfile` / `DeleteProfile` | ✅ M0 |
| 设备列表 | `Node.Peers`（名称、DNS 名、IP、OS、在线、最后在线、密钥过期、标签） | ✅ M0 |
| tailnet 隧道 | `Node.Dial` / `Node.Listen` / `Node.HTTPClient` / `Node.StartProxy`（本地 SOCKS5 + HTTP 代理，给非 Go 代码用） | ✅ M0 |
| 增强能力 | `Node.SetExitNode` / `ClearExitNode` / `Ping` / `WhoIs` | ✅ M0 |

## 快速开始

```bash
go build ./cli/tailnetctl            # 或: pwsh -File build/build.ps1 cli
```

```bash
# 1) 首次授权：打印登录 URL，用浏览器打开完成授权
dist/tailnetctl login -hostname tailnetsdk-dev

# 2) 状态与设备列表
dist/tailnetctl status
dist/tailnetctl peers

# 3) 隧道自测：A 端监听，B 端拨号（两个终端，-dir 必须不同）
dist/tailnetctl listen -hostname tailnetsdk-a -dir ./data/a :8080
dist/tailnetctl dial   -hostname tailnetsdk-b -dir ./data/b tailnetsdk-a:8080

# 4) 给非 Go 代码用的本地代理（SOCKS5 + HTTP，仅监听 127.0.0.1）
dist/tailnetctl proxy -hostname tailnetsdk-proxy

# 5) 注销
dist/tailnetctl logout
```

以 Go 库方式使用：

```go
node, err := core.New(core.Config{Hostname: "my-app"})
if err != nil { return err }
defer node.Close()

if err := node.Start(ctx); err != nil { return err }
if url, err := node.LoginURL(ctx); err == nil {
    openInBrowser(url) // Android Custom Tabs / iOS ASWebAuthenticationSession
}
st, err := node.WaitForRunning(ctx)   // 等待授权完成
conn, err := node.Dial(ctx, "tcp", "backend.tail-scale.ts.net:443")
```

## 目录结构

```
core/             平台无关 Go 核心（SDK 的唯一真相来源）
cli/tailnetctl/   冒烟/验收 CLI
build/            Makefile 与 Windows 构建脚本
docs/             设计文档
```

`core` 刻意**不暴露 tailscale.com 的公开类型**：所有返回值都是本仓库自己的 DTO（`Status`/`Peer`/`Event`/`Profile`/`Identity`/`PingResult`），升级 `tailscale.com` 时只需改动 `core/status.go` 的映射层，语言绑定的 ABI 不受影响。

## 平台注意事项

- **Windows / macOS**：直接 `go build`，无需管理员权限。
- **Android**：`core` 编译不需要 Android SDK；打包 AAR 需要 Android SDK/NDK + `gomobile bind`（M3）。
- **iOS / macOS framework**：需要 macOS + Xcode，用 `-buildmode=c-archive` + `-tags ios`（M4）。
- **后台存活**：用户态模式下进程被挂起即断连（iOS 尤其明显）。因此 SDK 的状态查询走**进程内 LocalAPI**（`Node.LocalClient`），不使用 loopback TCP 监听——后者在 iOS 挂起恢复后会失效。
- **代理与系统 VPN 的差异**：SDK 只让**显式经由 SDK 的连接**（Dial/Listen/HttpClient/SOCKS5 代理）走 tailnet，不会接管宿主 App 的其它流量；需要系统级 VPN 请走 TUN 路线（不在本期范围）。

## 版本与依赖

- Go 1.27.1（与 `tailscale.com` v1.102.4 的 `go` 指令一致）
- `tailscale.com v1.102.4`，BSD-3-Clause；本仓库同样遵循 BSD-3-Clause 的集成要求（保留版权声明，且不得使用 Tailscale 商标）

## 路线图

| 里程碑 | 内容 | 状态 |
|---|---|---|
| M0 | 仓库骨架、`core` 全部能力、`tailnetctl` CLI、构建/测试脚本 | ✅ |
| M1 | 状态机与事件流打磨、真实 tailnet 端到端验收、CI（含 `tstestcontrol` 本地控制面自动化测试） | 进行中 |
| M2 | Windows + macOS 桌面绑定（`c-shared`/`c-archive` + C#/Swift 封装） | 计划 |
| M3 | Android AAR（`gomobile bind`，先做可行性 POC，失败则回退手写 JNI） | 计划 |
| M4 | iOS/macOS xcframework + Swift 封装（`URLSession` 经本地 SOCKS5 访问 tailnet） | 计划 |
| M5（可选） | TUN 真 VPN：Android `VpnService`（`addAllowedApplication` 仅本 App）、Windows wintun、macOS/iOS Network Extension | 评估中 |
