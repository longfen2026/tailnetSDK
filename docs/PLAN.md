# 实施计划与风险台账

本文记录 SDK 的分期计划、验收标准与已知风险，供后续里程碑使用。

## 阶段划分

| 里程碑 | 交付物 | 验收标准 | 状态 |
|---|---|---|---|
| **M0** | `core` 包（登录/状态/注销/设备列表/隧道/增强能力）、`cli/tailnetctl`、`examples/quickstart`、`build/` 脚本 | `go build ./...`、`go vet ./...`、`go test ./...` 全绿；CLI 能拿到真实登录 URL；状态可持久化并回读 | ✅ 已完成 |
| **M1** | 状态机与事件流打磨；真实 tailnet 端到端验收（双节点 dial/listen + WhoIs）；CI 接入 `tstestcontrol` 本地控制面自动化测试 | 两个节点互相 `dial`/`listen` 成功；`WhoIs` 能解析对端身份；CI 在无外网凭据下可回归 | 进行中 |
| **M2** | `bind/ffi`（C ABI）+ Windows `c-shared` DLL + C# P/Invoke 封装 + macOS `c-archive`/xcframework + Swift 封装 | 示例程序无需安装 Tailscale、无需管理员权限即可登录并访问 tailnet 服务 | 计划 |
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
