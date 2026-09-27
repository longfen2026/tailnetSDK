# Tailnet SDK - Apple (macOS & iOS) 绑定

本模块提供针对 macOS 与 iOS 平台的 Swift / Objective-C 接入层及 XCFramework 构建脚本。

---

## 1. 架构与设计

- **底层引擎**：基于 `bind/ffi` 导出的 C ABI，利用 Go 的 `-buildmode=c-archive` 编译各架构静态库（macOS arm64/x86_64、iOS Device arm64、iOS Simulator arm64/x86_64）。
- **打包标准**：通过 `xcodebuild -create-xcframework` 封装为标准的 `Tailnet.xcframework`。
- **上层封装**：
  - `TailnetClient`：管理进程内节点生命周期、状态、认证和 TCP 桥接。
  - `URLSession.tailnetSession(client:)`：利用 SOCKS5 代理字典自动无缝拦截并路由 HTTP/HTTPS 流量，无需任何系统级 Network Extension 权限。

---

## 2. 构建 XCFramework（需 macOS 构建机）

在配备 Xcode 15+ 与 Go 1.21+ 的 macOS 设备上执行：

```bash
chmod +x bind/apple/build-xcframework.sh
./bind/apple/build-xcframework.sh
```

构建成功后将生成：
`dist/Tailnet.xcframework`

---

## 3. Swift 快速上手

### 3.1 初始化与登录

```swift
import TailnetKit

let client = TailnetClient()
let appSupport = FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask).first!
let stateDir = appSupport.appendingPathComponent("tailnet").path

// 1. 配置并启动
try client.configure(dir: stateDir, hostname: "my-mac-app")
try client.start()

// 2. 检查登录状态
let status = try client.status()
if status.state == "NeedsLogin" {
    try client.startLoginInteractive()
    if let authUrl = try client.authURL() {
        // 使用系统浏览器打开登录授权
        NSWorkspace.shared.open(authUrl) // macOS
        // UIApplication.shared.open(authUrl) // iOS
    }
}
```

---

### 3.2 使用 URLSession 访问 tailnet 服务

```swift
// 创建绑定了本地 SOCKS5 代理的 URLSession
let session = URLSession.tailnetSession(client: client)

// 直接发起请求（支持 MagicDNS 域名与 100.x IP）
let url = URL(string: "http://my-peer.tailnet:8080/api/status")!
let (data, response) = try await session.data(from: url)
print("Response: \(String(data: data, encoding: .utf8) ?? "")")
```
