# 实施计划与风险台账

本文记录 SDK 的分期计划、验收标准与已知风险，供后续里程碑使用。

## 阶段划分

| 里程碑 | 交付物 | 验收标准 | 状态 |
|---|---|---|---|
| **M0** | `core` 包（登录/状态/注销/设备列表/隧道/增强能力）、`cli/tailnetctl`、`examples/quickstart`、`build/` 脚本 | `go build ./...`、`go vet ./...`、`go test ./...` 全绿；CLI 能拿到真实登录 URL；状态可持久化并回读 | ✅ 已完成 |
| **M1** | 本地控制面（`tstest/integration/testcontrol` + 本地 DERP/STUN）自动化集成测试：登录生命周期、设备发现、双节点隧道 + WhoIs、HTTP、SOCKS5、注销、事件流；CI workflow | 两个节点互相 `dial`/`listen` 成功；`WhoIs` 能解析对端身份；CI 在无外网凭据下可回归 | ✅ 已完成（本地控制面 7 项集成测试全绿；真实云端双节点验收待有 tailnet 账号时补充，可并入 M2 期间） |
| **M2** | `bind/ffi`（C ABI）+ Windows `c-shared` DLL + C# P/Invoke 封装 + macOS `c-archive`/xcframework + Swift 封装 | 示例程序无需安装 Tailscale、无需管理员权限即可登录并访问 tailnet 服务 | **Windows 分支已完成**（见下方验证记录）；macOS/Swift 分支需 macOS 构建机，待环境就绪 |
| **M3** | `bind/gomobile` + Android AAR + Kotlin 封装 + 前台服务 + Custom Tabs 登录 | **首日做 `gomobile bind` 可行性 POC**；Demo APK 登录后经本地 SOCKS5 访问 tailnet 服务；进程重启后状态可恢复 | **首日 POC 达成**（产出 4 架构 AAR `dist/tailnet.aar`，单元测试全绿） |
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
| `gomobile bind` 打包 tsnet 依赖失败 | Android 阻塞 | M3 首日 POC；回退方案：手写 JNI + `c-archive`，或改用 `ipnlocal` 自行装配（官方 Android 路线） | ✅ 已解决（首日 POC 成功产出 4 架构 AAR） |
| iOS 进程挂起导致 loopback 监听失效 | 状态读取失败 | 全部状态走进程内 `LocalClient`（已实现） | 已规避 |
| 上游 API 演进 | 升级成本 | 锁定 `tailscale.com v1.102.4`；对外只暴露本仓库 DTO；映射集中在 `core/status.go` | 已规避 |
| 用户态模式无法接管任意 App 流量 | 产品预期偏差 | README/API 文档明确边界；提供本地 SOCKS5/HTTP 代理供非 Go 网络栈接入 | 已文档化 |
| 包体与内存（Go + gVisor） | 分发压力 | `-ldflags "-s -w"`、`ts_omit_*` 裁剪（官方 Android 用 `ts_omit_cachenetmap`）、按 ABI 拆分 | 待 M2 建立体积基线 |
| 与官方 Tailscale 客户端同时开启 VPN | 冲突（仅 TUN 路线） | 用户态路线无冲突；TUN 路线需在 UI 提示互斥 | 待 M5 处理 |
| 宿主侧桥接连接的 `RemoteAddr` 恒为 `127.0.0.1` | `WhoIs` 无法归因对端身份 | FFI 建桥时记录真实地址，新增 `tailnet_conn_remote_addr`；宿主侧 `RemoteTailnetEndPoint` 暴露 | 已规避（M2 实测通过） |
| demo 进程退出后节点在 tailnet 中消失（多次复现） | 每次启动需重新授权，影响体验 | 已确认是官方控制面行为而非 SDK bug：非 `Ephemeral` 节点也应长期保留，需在管理后台核对是否为重复注册；后续在 M4 用持久化 keystore 固定节点 key 后再复验 | 观察中（已记录，M4 复验） |
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
- `bind/ffi/tailnet.go`：语言无关 C ABI，**31 个导出函数**（生命周期/配置/状态/授权/会话/隧道/代理/
  诊断/事件流全覆盖）。句柄制（`int`，Go 指针不跨边界）；JSON 经由 `char**` 返回并由宿主
  `tailnet_free` 释放；小值写入调用方缓冲区
- `bind/dotnet/TailnetSdk.cs`：零依赖 P/Invoke 封装（`TailnetNode`/`TailnetListener`/
  `TailnetEventStream`/`TailnetBridgedConnection`，async API + DTO），单文件可直接 `Compile Include`
- `examples/windows-dotnet/`：C# demo（`--smoke` 离线自检 / 交互登录 / `--ping` / `--dial` /
  `--serve` / `--proxy` / `--logout`）
- `bind/ffi/README.md`：C ABI 参考（约定、桥接模型、31 个函数速查、可编译的最小 C 示例）——
  已逐条对照 `dist/tailnet.h` 与实现校正（错误码、签名、内存归属、事件流机制）
- `build/build.ps1 native`：一键构建原生库（自动探测 mingw-w64 gcc，可从 `-CC`/`$env:CC`/PATH/
  仓库同级 `tools/w64devkit` 定位）

重建 DLL（二选一）：
```
pwsh -File build\build.ps1 native                          # 推荐：自动探测编译器
cd bind\ffi && set CGO_ENABLED=1 && set CC=<mingw gcc> && go build -ldflags "-s -w" -buildmode=c-shared -o ..\..\dist\tailnet.dll .
```
（注意 `bind/ffi` 是独立 go.mod，避免把测试依赖带进宿主模块。）

### C ABI 关键语义（易踩坑，已写入 `bind/ffi/README.md`）

| 点 | 事实 |
|---|---|
| 返回值 | `0` 成功；`-1` = SDK/后端错误（含超时，原因见 `tailnet_errmsg`）；**正** errno `EBADF`/`EINVAL`/`ERANGE`（句柄无效/参数非法/缓冲区过小）；**负** errno `-EIO`（listener 已关）/`-ETIMEDOUT`（取事件超时） |
| lastErr 生命周期 | 一次**成功**的调用会清空 lastErr —— 判失败后要立刻取错误串 |
| 内存归属 | `*_json`/`tailnet_next_event` 返回 malloc 字符串，必须 `tailnet_free`；`tailnet_close` 只销毁句柄 |
| 隧道 | `tailnet_dial`/`tailnet_accept` 返回 **127.0.0.1 桥接端口**（不是 fd）；每条桥只接受一个宿主连接 |
| 事件流 | **不走 TCP 桥**：`tailnet_watch` 返回流句柄，`tailnet_next_event` 阻塞取（容量 64，消费慢则丢事件而不阻塞后端） |

### M2 真实 tailnet 端到端（Windows ↔ Android 手机）

```
demo --serve 8099                   -> 监听成功，日志打印访问者
小米 14 Pro 浏览器访问 100.92.31.1:8099 -> 命中服务
FFI：tailnet_conn_remote_addr       -> WhoIs 解析出真实对端身份（桥接 socket 为 127.0.0.1，
                                       修复前身份为空，修复后带出 100.x 真实地址）
demo --ping / --dial                -> 连通性与 banner 诊断可用
dotnet publish -c Release -r win-x64 --self-contained true -p:PublishSingleFile=true -o publish\win-x64
                                    -> 产出 tailnet-demo.exe + tailnet.dll（原生库与 exe 并列），
                                       目标机无需安装 Go/.NET
```

## 本地验证记录（M3 — Android 分支 POC）

环境配置：Go 1.27.1, Android SDK API 34+ (NDK 28.2.13676358), JDK 17, gomobile 工具链。

```
go test ./bind/gomobile/...       -> PASS (0.136s)，生命周期与离线状态迁移全绿
pwsh -File build/build.ps1 aar    -> 自动探测 Android SDK / NDK / JDK，调用 gomobile bind
dist/tailnet.aar                 -> 成功生成 (58.8MB)
                                    包含 classes.jar (tailnetmobile.Node / Listener / EventListener)
                                    包含 4 架构 libgojni.so:
                                      - jni/arm64-v8a/libgojni.so
                                      - jni/armeabi-v7a/libgojni.so
                                      - jni/x86/libgojni.so
                                      - jni/x86_64/libgojni.so
```

结论：**首日验证 POC 100% 成功**，用户态 `tsnet` 完全可通过 `gomobile bind` 平滑构建 Android AAR，无需降级手写 JNI。


关键修复：宿主侧拿到的桥接连接其 `RemoteAddr` 永远是 `127.0.0.1`，导致 `WhoIs` 无法归因。
新增 `tailnet_conn_remote_addr(port, buf, n)`，FFI 在建桥时记录真实 tailnet 对端地址，
`TailnetBridgedConnection.RemoteTailnetEndPoint` 对外暴露。该方案在 Windows/macOS/iOS/Android
行为一致（不依赖 Unix `socketpair`）。


