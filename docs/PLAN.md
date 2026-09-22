# 实施计划与风险台账

本文记录 SDK 的分期计划、验收标准与已知风险，供后续里程碑使用。

## 阶段划分

| 里程碑 | 交付物 | 验收标准 | 状态 |
|---|---|---|---|
| **M0** | `core` 包（登录/状态/注销/设备列表/隧道/增强能力）、`cli/tailnetctl`、`examples/quickstart`、`build/` 脚本 | `go build ./...`、`go vet ./...`、`go test ./...` 全绿；CLI 能拿到真实登录 URL；状态可持久化并回读 | ✅ 已完成 |
| **M1** | 本地控制面（`tstest/integration/testcontrol` + 本地 DERP/STUN）自动化集成测试：登录生命周期、设备发现、双节点隧道 + WhoIs、HTTP、SOCKS5、注销、事件流；CI workflow | 两个节点互相 `dial`/`listen` 成功；`WhoIs` 能解析对端身份；CI 在无外网凭据下可回归 | ✅ 已完成（本地控制面 7 项集成测试全绿；真实云端双节点验收待有 tailnet 账号时补充，可并入 M2 期间） |
| **M2** | `bind/ffi`（C ABI）+ Windows `c-shared` DLL + C# P/Invoke 封装 + macOS `c-archive`/xcframework + Swift 封装 | 示例程序无需安装 Tailscale、无需管理员权限即可登录并访问 tailnet 服务 | **Windows 分支已完成**（见下方验证记录）；macOS/Swift 分支需 macOS 构建机，待环境就绪 |
| **M3** | `bind/gomobile` + Android AAR + Kotlin 封装 + 前台服务 + Custom Tabs 登录 | **首日做 `gomobile bind` 可行性 POC**；Demo APK 登录后经本地 SOCKS5 访问 tailnet 服务；进程重启后状态可恢复 | 计划 |
| **M4** | iOS/macOS xcframework + Swift `TailnetClient` + `URLSession` 走本地 SOCKS5（参考 upstream `libtailscale/swift` 的构建脚本与 TailscaleKit 的 URLSession 设计） | 真机登录并访问 tailnet 服务；模拟器/真机 framework 均可构建；App 挂起恢复后状态读取正常 | 计划 |
| **M5（可选）** | TUN 真 VPN：Android `VpnService`（`addAllowedApplication` 仅本 App 流量）、Windows wintun、macOS/iOS Network Extension | 客户端获得 tailnet IP；exit node / subnet router 生效 | 评估中 |

## 已确认的关键上游事实（v1.102.4）

- `tsnet.Server` 提供 `LocalClient()`，其内部 LocalAPI **已开启 `PermitWrite`**，因此登录、注销、改配置等写操作在进程内可用。
- `login-interactive` 端点已注册（`ipn/localapi` 第 83 行），`local.Client.StartLoginInteractive/Logout/WatchIPNBus/EditPrefs/SetUseExitNode/ProfileStatus` 均可用。
- 登录 URL 有三种获取途径：`StartLoginInteractive` 触发 + `Status.AuthURL`、`Watch` 的 `BrowseToURL`、以及 `Config.UserLogf` 日志。
- `tsnet` 源码含 `case "ios", "darwin"` 分支，说明官方已支持 iOS。
- `tsnet.Server.Tun`（自定义 TUN）自 v1.102.4 起存在，是 M5 走 TUN 路线的基础；但 `tsnet` 内部不注入自定义 Router/DNS，因此 TUN 模式需要 fork `tsnet.go` 或改用 `tsd.System` + `ipnlocal` 自行装配（参考 `tailscale-android/libtailscale`）。
- 官方 iOS/macOS GUI 为闭源（issue #13717），Android 端开源，可参考其 `gomobile bind` 与 VpnService 做法。

## 风险台账

| 风险 | 影响 | 对策 | 状态 |
|---|---|---|---|
| `gomobile bind` 打包 tsnet 依赖失败 | Android 阻塞 | M3 首日 POC；回退方案：手写 JNI + `c-archive`，或改用 `ipnlocal` 自行装配（官方 Android 路线） | 未验证 |
| iOS 进程挂起导致 loopback 监听失效 | 状态读取失败 | 全部状态走进程内 `LocalClient`（已实现） | 已规避 |
| 上游 API 演进 | 升级成本 | 锁定 `tailscale.com v1.102.4`；对外只暴露本仓库 DTO；映射集中在 `core/status.go` | 已规避 |
| 用户态模式无法接管任意 App 流量 | 产品预期偏差 | README/API 文档明确边界；提供本地 SOCKS5/HTTP 代理供非 Go 网络栈接入 | 已文档化 |
| 包体与内存（Go + gVisor） | 分发压力 | `-ldflags "-s -w"`、`ts_omit_*` 裁剪（官方 Android 用 `ts_omit_cachenetmap`）、按 ABI 拆分 | 待 M2 建立体积基线 |
| 与官方 Tailscale 客户端同时开启 VPN | 冲突（仅 TUN 路线） | 用户态路线无冲突；TUN 路线需在 UI 提示互斥 | 待 M5 处理 |
| 双节点 TCP 数据在 DERP/DISCO 握手完成前可能被静默丢弃 | 集成测试假失败（连接已建立但无数据） | 建链前先做 TSMP Ping 预热（`waitReachable`），对齐上游 tsnet 测试做法 | 已规避 |
| 控制面为官方 | 账号/设备额度、ACL、Tailnet Lock 均受官方约束 | 文档说明；`Config.ControlURL` 保留切换自建控制面的能力 | 已文档化 |

## 本地验证记录（M0）

```
go build ./...   -> BUILD_OK
go vet ./...     -> VET_OK
go test ./...    -> ok tailnetsdk/core 0.115s
cli 版本/帮助     -> 正常输出
真实联调          -> tailnetctl login -url-only 取得 https://login.tailscale.com/a/fe6d90a0170c3
                     tailnetctl status -json 返回 state=NeedsLogin, tun=false（状态已持久化于 data/m0）
```

`data/` 存放节点状态（含节点私钥），已在 `.gitignore` 中排除；如需重置本机测试身份，删除对应目录即可。

## 本地验证记录（M1）

`go test ./core/ -count=1 -v -timeout 15m`：本地控制面（testcontrol）+ 本地 DERP/STUN，
全程离线、无需任何 Tailscale 账号或凭据：

```
--- PASS: TestStateFromIPN / TestMapStatusAndPeers / TestMapStatusNilSafe / TestFindPeer / TestEventJSON / TestConfigDefaults / TestErrorCode
--- PASS: TestIntegrationAuthLifecycle          (1.09s) 登录→Running、用户态(TUN=false)、100.64/10 地址、MagicDNS、Profile.ControlURL、已授权时 LoginURL 报错
--- PASS: TestIntegrationPeerDiscovery          (1.40s) 双节点互见（tailnet IP + DNS 名）
--- PASS: TestIntegrationTunnelEcho             (1.42s) MagicDNS 拨号 sdk-a:8080 + echo 回环 + WhoIs 归因
--- PASS: TestIntegrationHTTPClientOverTailnet  (1.38s) Node.HTTPClient 请求 tailnet 内 HTTP 服务
--- PASS: TestIntegrationSOCKS5Proxy            (1.37s) golang.org/x/net/proxy 经本地 SOCKS5 访问 tailnet 服务
--- PASS: TestIntegrationLogout                 (0.75s) 注销后离开 Running
--- PASS: TestIntegrationWatchEvents            (0.74s) Watch 事件流（state/self/peers）
ok  tailnetsdk/core  8.276s    （build + vet 同绿）
```

运行方式：`go test ./core/ -run TestIntegration -count=1`；快速跳过：`go test -short ./core/`。
CI：`.github/workflows/ci.yml` 三平台构建 + ubuntu 集成测试 job。

## 本地验证记录（M2 — Windows 分支）

工具链：w64devkit (mingw-w64 gcc) → `tools/w64devkit/`；.NET SDK 8.0.425（用户级安装）。

```
go build -buildmode=c-shared  -> dist/tailnet.dll (22.2MB) + dist/tailnet.h
PowerShell P/Invoke 冒烟       -> SMOKE_OK（version→new→configure→start→status(NeedsLogin)→close）
C# demo (dotnet build)        -> 0 警告 0 错误
C# demo (dotnet run --smoke)  -> state=NeedsLogin，
                                 取得真实登录 URL https://login.tailscale.com/a/2a8f830141dc
```

交付物：
- `bind/ffi/tailnet.go`：语言无关 C ABI（句柄制；configure/status/login/tunnel/proxy/events 全覆盖，
  JSON 字符串 + `int` 句柄 + 管道 fd 传 socket，供 Kotlin/Swift/C#/Python 等任意宿主调用）
- `bind/dotnet/TailnetSdk.cs`：零依赖 P/Invoke 封装（`TailnetNode`/`TailnetListener`/
  `TailnetEventStream`，async API + DTO）
- `examples/windows-dotnet/`：C# demo（`--smoke` 无账号验证 / 交互登录 / `--logout` / `--proxy`）

重建 DLL：`bind/ffi` 目录下
`CGO_ENABLED=1 CC=<mingw gcc> go build -ldflags "-s -w" -buildmode=c-shared -o ../../dist/tailnet.dll .`
（注意 `bind/ffi` 是独立 go.mod，避免把测试依赖带进宿主模块。）

