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
bind/ffi/         语言无关 C ABI（Windows tailnet.dll / macOS·iOS c-archive）
bind/dotnet/      零依赖 C# P/Invoke 封装（TailnetNode / TailnetListener / TailnetEventStream）
bind/gomobile/    Android / 移动端 AAR 绑定（gomobile bind 产物 tailnet.aar）
bind/apple/       macOS & iOS Swift 接入与 XCFramework 构建脚本 (TailnetKit)
examples/         接入示例（quickstart 为 Go；windows-dotnet 为 .NET）
build/            Makefile 与 Windows 构建脚本
docs/             设计文档
```

`core` 刻意**不暴露 tailscale.com 的公开类型**：所有返回值都是本仓库自己的 DTO（`Status`/`Peer`/`Event`/`Profile`/`Identity`/`PingResult`），升级 `tailscale.com` 时只需改动 `core/status.go` 的映射层，语言绑定的 ABI 不受影响。

## Windows / .NET 接入

```powershell
# 1) 构建原生库（需要 mingw-w64 系 gcc，例如 w64devkit）
pwsh -File build\build.ps1 native      # -> dist\tailnet.dll + dist\tailnet.h
# 若 gcc 不在 PATH：pwsh -File build\build.ps1 native -CC C:\tools\w64devkit\w64devkit\bin\gcc.exe

# 2) 构建并运行 C# demo（.NET 8+）
cd examples\windows-dotnet
dotnet run -- --smoke                  # 离线自检：托管 -> 原生全链路，不联网
dotnet run                             # 交互登录 + 设备列表 + 隧道诊断

# 3) 独立分发：self-contained 单文件，目标机无需安装 Go / .NET
dotnet publish -c Release -r win-x64 --self-contained true -p:PublishSingleFile=true -o publish\win-x64
#   产出：publish\win-x64\tailnet-demo.exe + tailnet.dll（原生库与 exe 并列，DllImport 直接命中）
```

demo 的常用开关：

| 开关 | 作用 |
|---|---|
| `--smoke` | 仅校验 P/Invoke 链路，不访问控制面 |
| `--dir <path>` | 状态目录（默认 `%TEMP%\tailnet-demo`）；节点身份与登录态存于此 |
| `--hostname <name>` | tailnet 中的设备名（默认 `demo-win`） |
| `--ping <tailnet IP>` | TSMP 连通性诊断（等价 `tailscale ping`） |
| `--dial <host:port>` | 打开一条 tailnet TCP 连接并打印 banner |
| `--serve <port>` | 把本机 HTTP 服务暴露给 tailnet（响应中带回访问者身份，走 `WhoIs`） |
| `--proxy` | 打印本地 SOCKS5/HTTP 代理地址，供非 Go 网络栈接入 |
| `--stay <seconds>` | 保持连接指定秒数后自动退出 |
| `--logout` | 注销并退出 |

C# 侧最小用法（`Dir` 为必填：App 必须自己拥有可写的状态目录）：

```csharp
using System.Net.Sockets;
using Tailnet;

var opt = new TailnetOptions { Dir = dataDir, Hostname = "my-app" };
using var node = new TailnetNode(opt);
await node.StartAsync();

var st = await node.GetStatusAsync();
if (!st.IsRunning())
{
    string url = await node.GetLoginUrlAsync();              // 交给系统浏览器打开
    await node.WaitForRunningAsync(TimeSpan.FromMinutes(5)); // 授权完成后自动返回
}

using TcpClient tcp = await node.DialAsync("backend:443", TimeSpan.FromSeconds(10));

## Android 接入 (AAR)

```powershell
# 构建 AAR 原生库（自动探测 Android SDK / NDK / JDK）
pwsh -File build\build.ps1 aar          # -> dist\tailnet.aar
```

产物包含 `classes.jar` 及全部 4 种 ABI（`arm64-v8a`、`armeabi-v7a`、`x86`、`x86_64`）的 `libgojni.so`。

Kotlin 最小接入：
```kotlin
// 1. 初始化并启动
val node = Tailnetmobile.newNode()
node.configure(context.filesDir.resolve("tailnet").absolutePath, "my-android", "", "", false)
node.start()

// 2. 授权检查（配合 Custom Tabs 打开）
if (node.state() == "NeedsLogin") {
    node.startLoginInteractive()
    val authUrl = node.authURL()
    CustomTabsIntent.Builder().build().launchUrl(context, Uri.parse(authUrl))
}

// 3. 网络请求无缝走 tailnet（无需系统 VPN 权限）
val okHttpClient = OkHttpClient.Builder()
    .proxy(Proxy(Proxy.Type.SOCKS, InetSocketAddress("127.0.0.1", node.socks5Addr().split(":")[1].toInt())))
    .build()
```
详见 [bind/gomobile/README.md](bind/gomobile/README.md)。

## macOS / iOS 接入 (Swift / XCFramework)

```bash
# 在 macOS 上构建 XCFramework（包含 macOS 与 iOS Simulator/Device）
chmod +x bind/apple/build-xcframework.sh
./bind/apple/build-xcframework.sh        # -> dist/Tailnet.xcframework
```

Swift 最小接入：
```swift
import TailnetKit

let client = TailnetClient()
try client.configure(dir: stateDir, hostname: "my-apple-app")
try client.start()

// 让系统标准 URLSession 自动走 tailnet（SOCKS5 驱动）
let session = URLSession.tailnetSession(client: client)
let (data, _) = try await session.data(from: URL(string: "http://my-peer.tailnet:8080/api")!)
```
详见 [bind/apple/README.md](bind/apple/README.md)。


```

`bind/dotnet/TailnetSdk.cs` 是**单文件、零 NuGet 依赖**的封装，可直接 `Compile Include` 进任意 .NET 工程；`TailnetListener.AcceptAsync()` 返回的 `TailnetBridgedConnection.RemoteTailnetEndPoint` 携带**真实 tailnet 对端地址**（桥接 socket 本身是 `127.0.0.1`），可直接交给 `WhoIsAsync` 做身份归因。

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
| M1 | 本地控制面自动化集成测试（登录生命周期/设备发现/双节点隧道+WhoIs/HTTP/SOCKS5/注销/事件流）、CI workflow | ✅ |
| M2 | Windows + macOS 桌面绑定（`c-shared`/`c-archive` + C#/Swift 封装） | Windows ✅（DLL + C# 封装 + demo 已验证）；macOS 待 macOS 构建机 |
| M3 | Android AAR（`gomobile bind`，先做可行性 POC，失败则回退手写 JNI） | 计划 |
| M4 | iOS/macOS xcframework + Swift 封装（`URLSession` 经本地 SOCKS5 访问 tailnet） | 计划 |
| M5（可选） | TUN 真 VPN：Android `VpnService`（`addAllowedApplication` 仅本 App）、Windows wintun、macOS/iOS Network Extension | 评估中 |
