# 节点生命周期与持久化机制 (Node Persistence Semantics)

本文档阐述用户态节点（`tsnet`）在 Tailscale 控制面中的注册、保活、离线与回收行为，以及如何在宿主 App 中实现稳定的节点身份持久化。

---

## 1. 现象分析：为什么节点有时在控制台“消失”？

在开发或测试过程中，开发者有时会观察到：
> Demo 进程退出后，在 Tailscale 管理控制台（Admin Console）中，该节点很快显示离线甚至消失，再次启动时又需要重新登录。

### 根本原因

1. **状态目录非固定（随机 `%TEMP%` 目录）**：
   - `tsnet.Server` 的身份与密钥持久化完全依赖于配置中的 `Dir` 目录（内部保存为 `tailscaled.state`）。
   - 如果每次启动都使用了随机临时目录（例如测试时传入了动态的 `%TEMP%\demo-xxx`），控制面就会认为这是一个**全新安装的硬件设备**，发起全新的登录流程（返回新的 `AuthURL`）。
   - 之前由临时目录生成的旧节点因私钥丢失且长期未发起心跳，若属于临时节点，会被控制面回收。

2. **临时节点标志 (`Ephemeral`)**：
   - 若 `Config.Ephemeral = true`（或使用的 AuthKey 带有 `ephemeral` 属性），控制面在检测到该节点 TCP/DERP 断开一段时间后，就会自动将其从机器列表中彻底删除（reap）。

3. **官方控制面的设备额度与自动清理**：
   - 官方 Tailscale 免费版对活跃设备数量有配额限制。对于未授权（NeedsLogin）或者短命的无活动临时设备，控制面存在定期的清理策略。

---

## 2. 解决方案与持久化设计规范

为了在不同平台上确保 App 重启后**免登录、身份稳定、无需重新授权**，请严格遵守以下集成规范：

### 2.1 状态目录选择

| 平台 | 推荐状态路径 (`Dir`) | 特性说明 |
|---|---|---|
| **Windows** | `%LOCALAPPDATA%\<AppName>\tailnet` | 用户级持久化，应用升级或重启不受影响 |
| **Android** | `context.filesDir.resolve("tailnet").absolutePath` | 随应用沙盒持久化，系统清理临时缓存时不会丢失 |
| **macOS** | `~/Library/Application Support/<BundleId>/tailnet` | 遵循 macOS 规范，沙盒内外均可稳定保存 |
| **iOS** | `FileManager.default.urls(for: .applicationSupportDirectory, ...)/tailnet` | 备份与持久化沙盒目录 |

> ⚠️ **警告**：切勿将 `Dir` 设在操作系统的临时缓存目录（如 `NSTemporaryDirectory()` 或 `Path.GetTempPath()`），否则系统随时可能清理私钥导致节点重新登录。

### 2.2 保持 `Ephemeral = false`

除非是无状态的 CI Runner 或一次性自动化批处理任务，客户端应用一律设置 `Ephemeral: false`，告知控制面这是一个长期稳定的宿主设备。

### 2.3 状态检查最佳实践

App 启动时执行如下流程：
```
配置固定 Dir -> 调用 Start() -> 获取 Status()
  |-- 若 State == "Running": 免登成功，直接使用隧道与代理
  |-- 若 State == "NeedsLogin": 首次运行或凭据过期，弹出系统浏览器完成 OAuth 授权
```
一旦首次授权成功，`tailscaled.state` 会将设备认证票据妥善持久化，下次启动无需任何用户交互即可直接进入 `Running`。
