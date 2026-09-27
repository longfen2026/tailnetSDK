# Tailnet SDK - Android (gomobile AAR) 绑定

本模块使用 `gomobile bind` 将 `core` 引擎打包为标准 Android AAR 库（`dist/tailnet.aar`）。
适用于 Android 8.0+ (API 26+)，原生支持 **arm64-v8a**、**armeabi-v7a**、**x86** 和 **x86_64** 架构。

---

## 1. 构建 AAR

要求环境：Go 1.21+、Android SDK (API 26+)、Android NDK (r25+)、JDK 17。

```powershell
# 自动探测 Android SDK / NDK / JDK 并打包产出 dist\tailnet.aar
pwsh -File build\build.ps1 aar
```

产出：`dist/tailnet.aar`（内含 classes.jar 与全部 4 架构的 `libgojni.so`）。

---

## 2. Android 工程接入

### 2.1 引入 AAR

将 `dist/tailnet.aar` 拷贝到 Android 工程模块的 `libs/` 目录下，并在 `build.gradle.kts` 中添加依赖：

```kotlin
dependencies {
    implementation(files("libs/tailnet.aar"))
}
```

并在 `AndroidManifest.xml` 中声明必要权限：

```xml
<uses-permission android:name="android.permission.INTERNET" />
<uses-permission android:name="android.permission.ACCESS_NETWORK_STATE" />
<!-- 如果需要在后台保活，推荐声明前台服务 -->
<uses-permission android:name="android.permission.FOREGROUND_SERVICE" />
<uses-permission android:name="android.permission.FOREGROUND_SERVICE_DATA_SYNC" />
```

---

## 3. Kotlin 快速上手

### 3.1 节点生命周期与登录

```kotlin
import tailnetmobile.Node
import tailnetmobile.Tailnetmobile
import tailnetmobile.EventListener
import android.net.Uri
import androidx.browser.customtabs.CustomTabsIntent

class TailnetManager(private val context: Context) {
    private val node = Tailnetmobile.newNode()
    private var sub: tailnetmobile.WatchSubscription? = null

    fun start() {
        // 1. 配置节点（状态保存在 App 私有目录）
        val stateDir = File(context.filesDir, "tailnet").absolutePath
        node.configure(stateDir, "android-client", "", "", false)

        // 2. 监听事件流（状态变化、登录 URL、节点发现）
        sub = node.watch(object : EventListener {
            override fun onEvent(jsonEvent: String) {
                println("Tailnet Event: $jsonEvent")
            }
        })

        // 3. 启动节点
        node.start()

        // 4. 检查是否需要登录
        val state = node.state() // "NeedsLogin", "Running", "NoState" 等
        if (state == "NeedsLogin") {
            node.startLoginInteractive()
            val authUrl = node.authURL()
            if (!authUrl.isNullOrEmpty()) {
                // 使用 Chrome Custom Tabs 弹出授权页面
                CustomTabsIntent.Builder().build().launchUrl(context, Uri.parse(authUrl))
            }
        }
    }

    fun stop() {
        sub?.close()
        node.close()
    }
}
```

---

### 3.2 网络访问：OkHttp 走 tailnet

SDK 启动后会自动拉起本地用户态代理（SOCKS5 + HTTP）。OkHttp 无需系统 VPN 权限即可直接访问 tailnet 内网节点或服务：

```kotlin
import okhttp3.OkHttpClient
import java.net.InetSocketAddress
import java.net.Proxy

fun createTailnetOkHttpClient(node: Node): OkHttpClient {
    val proxyAddr = node.socks5Addr() // 例如 "127.0.0.1:41235"
    val parts = proxyAddr.split(":")
    val host = parts[0]
    val port = parts[1].toInt()

    return OkHttpClient.Builder()
        .proxy(Proxy(Proxy.Type.SOCKS, InetSocketAddress(host, port)))
        .build()
}
```

---

### 3.3 原始 TCP 拨号与监听

- **拨号**：调用 `val localPort = node.dial("tcp", "my-server:8080", 10000)`，建立连接后返回 `127.0.0.1` 上的本地桥端口，Java 使用标准 `java.net.Socket("127.0.0.1", localPort)` 即可收发数据。
- **监听**：调用 `val listener = node.listen("tcp", ":8080")`，在独立协程/线程中循环调用 `val port = listener.accept()` 处理入站请求。
- **身份识别**：对端连接接入后，可调用 `node.remoteTailnetAddr(port)` 取得其真实的 100.x tailnet IP 并调用 `node.whoIs(addr)` 解析对方身份。
