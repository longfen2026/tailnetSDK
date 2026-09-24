# `bind/ffi` — tailnetSDK C ABI

`bind/ffi` 把 `core` 暴露成**纯 C ABI**，任何具备 C FFI 的语言都能嵌入一个 tailnet 节点：
.NET（P/Invoke）、Swift（`c-archive` 头文件）、Kotlin/JNI、Python（ctypes/cffi）、Ruby（Fiddle）等。

命名与语义尽量对齐上游 `tailscale.com/libtailscale`，但**有一处关键差异**（见下节“桥接模型”）。

## 构建

```bash
# Windows 动态库：产出 tailnet.dll + tailnet.h
cd bind/ffi
set CGO_ENABLED=1
set CC=C:\path\to\w64devkit\bin\gcc.exe    # 需要 mingw-w64 系 GCC
go build -buildmode=c-shared -ldflags "-s -w" -o ..\..\dist\tailnet.dll .

# macOS / iOS 静态库（用于 xcframework，需 macOS 构建机）
GOOS=darwin GOARCH=arm64 CGO_ENABLED=1 go build -buildmode=c-archive -o dist/tailnet.a .
```

仓库根目录的 `build/build.ps1`（Windows）与 `build/Makefile` 已封装上述命令。

## ABI 约定

| 约定 | 说明 |
|---|---|
| **句柄** | 节点是 `int` 句柄（`tailnet_new()` 返回，永不为 0）；监听器句柄（`99<<16+n`）与事件流句柄（`77<<16+n`）也是 `int`。Go 指针从不跨边界。 |
| **返回值** | `0` = 成功。失败分三类：<br>① `-1` = SDK/后端错误，详情用 `tailnet_errmsg(sd, buf, n)` 取（含超时）；<br>② **正** errno：`EBADF`（句柄无效）、`EINVAL`（参数非法）、`ERANGE`（调用方缓冲区过小）；<br>③ **负** errno：`-EIO`（listener 已关闭 / 桥接失败）、`-ETIMEDOUT`（`tailnet_next_event` 超时）。 |
| **错误信息** | 每个节点保存最后一次错误字符串；注意**一次成功的调用会把它清空**，所以要在判失败后立刻取。 |
| **内存归属** | 返回 `char*` 的 JSON 函数（`*_json`/`tailnet_next_event`）使用 `malloc`，**必须由调用方 `tailnet_free(p)` 释放**；`tailnet_close` 只负责句柄，不要传给它。写入调用方缓冲区的函数（`tailnet_getips`/`tailnet_login_url`/`tailnet_proxy_addrs`/`tailnet_errmsg`/`tailnet_version`/`tailnet_conn_remote_addr`）不分配内存，缓冲区过小返回 `ERANGE`。 |
| **超时** | 未显式传超时的调用内部统一 30s 上限；`timeoutMS <= 0` 表示"用默认 30s"。宿主 UI 线程不会被后端永久卡住。 |
| **线程** | 句柄可跨线程调用（内部加锁）；`tailnet_next_event` 应在单一读线程轮询。 |

## 桥接模型（与 libtailscale 的差异）

`libtailscale` 用 Unix `socketpair` 传递 `dial`/`listen` 的 fd。**Windows 与移动端做不到**：Go 运行时的 fd 与宿主 CRT 的 fd 不是同一套编号。

因此本 SDK 的隧道 API 走 **loopback TCP 桥**：

```
tailnet_dial(sd, net, addr, tmo, &port)  ->  桥接端口 P（宿主连接 127.0.0.1:P 即得到一条通往 tailnet 的 TCP 连接）
tailnet_accept(listener, &port)          ->  桥接端口 P（宿主连接 127.0.0.1:P 即得到对端连接）
```

- 桥只监听 `127.0.0.1`，不对外暴露；每条桥**只接受一个**宿主连接，用完即关。
- 桥的宿主侧对端地址永远是 `127.0.0.1`，**不是**真实 tailnet 地址。真实地址用
  `tailnet_conn_remote_addr(P, buf, n)` 取（**连接存活期间有效**，关闭后返回 `EBADF`），再交给
  `tailnet_whois_json` 解析身份。这条路径在 Windows / macOS / iOS / Android 上行为一致。
- **事件流不走 TCP 桥**：`tailnet_watch` 返回一个**事件流句柄**，宿主用阻塞式
  `tailnet_next_event(stream, timeoutMS, &out)` 逐条取（每次返回一个 malloc 的 JSON 事件，
  用 `tailnet_free` 释放），`tailnet_stop_watch` 结束订阅。事件队列容量 64，宿主消费过慢时
  **丢弃新事件而不阻塞后端**。


## 函数速查

### 生命周期
| 函数 | 说明 |
|---|---|
| `int tailnet_new(void)` | 分配节点句柄 |
| `int tailnet_configure(int sd, char* cfg)` | 一次性配置；`cfg` 是 JSON，字段：`dir`(必填) / `hostname` / `controlURL` / `ephemeral` / `authKey` / `enableProxy` / `advertiseTags` |
| `int tailnet_start(int sd)` | 启动后端并立即返回（不等待授权） |
| `int tailnet_up(int sd, int timeoutMS)` | 启动并阻塞等待就绪；失败返回 `-1`（超时原因见 `tailnet_errmsg`） |
| `int tailnet_close(int sd)` | 关闭后端并销毁句柄；之后句柄不可再用 |
| `void tailnet_free(char* p)` | 释放 SDK 返回的 JSON（`malloc` 出的指针） |
| `int tailnet_version(char* buf, size_t n)` | SDK 版本写入调用方缓冲区 |

### 授权与会话
| 函数 | 说明 |
|---|---|
| `int tailnet_status_json(int sd, char** out)` | 完整状态（含设备列表）JSON |
| `int tailnet_peers_json(int sd, char** out)` | 设备列表 JSON |
| `int tailnet_login_url(int sd, char* buf, size_t n)` | 登录 URL；**已授权时返回空串（成功）** |
| `int tailnet_start_login_interactive(int sd)` | 主动触发交互登录 |
| `int tailnet_wait_running(int sd, int timeoutMS, char** out)` | 阻塞等待 Running；成功时 `*out` 得到状态 JSON |
| `int tailnet_logout(int sd)` | 注销 |
| `int tailnet_profiles_json(int sd, char** out)` | 当前 + 全部登录 profile |
| `int tailnet_switch_profile(int sd, char* id)` / `tailnet_delete_profile(int sd, char* id)` | 切换 / 删除 profile |

### 隧道
| 函数 | 说明 |
|---|---|
| `int tailnet_dial(int sd, char* network, char* addr, int timeoutMS, int* portOut)` | 拨号，返回桥接端口 |
| `int tailnet_listen(int sd, char* network, char* addr, int* listenerOut)` | 在 tailnet 上监听（`addr` 形如 `:8080`） |
| `int tailnet_accept(int lh, int* portOut)` | 接受一条连接，返回桥接端口；listener 关闭时返回 `-EIO` |
| `int tailnet_conn_remote_addr(int port, char* buf, size_t size)` | 桥的真实 tailnet 对端地址 |
| `int tailnet_listener_close(int lh)` | 关闭监听器（可解除阻塞中的 `tailnet_accept`） |
| `int tailnet_proxy_addrs(int sd, char* addrBuf, size_t addrN, char* credBuf, size_t credN)` | 本地 SOCKS5/HTTP 代理地址与口令（未启动则自动启动） |

### 诊断与增强
| 函数 | 说明 |
|---|---|
| `int tailnet_getips(int sd, char* v4Buf, size_t v4N, char* v6Buf, size_t v6N)` | 本机 tailnet IP（未授权时为空串） |
| `int tailnet_ping_json(int sd, char* addr, char** out)` | TSMP ping（等价 `tailscale ping`） |
| `int tailnet_whois_json(int sd, char* remoteAddr, char** out)` | 连接对端身份 |
| `int tailnet_set_exit_node(int sd, char* nodeID)` / `tailnet_clear_exit_node(int sd)` | exit node |
| `int tailnet_errmsg(int sd, char* buf, size_t n)` | 取该节点最后一次错误 |

### 事件流
| 函数 | 说明 |
|---|---|
| `int tailnet_watch(int sd, int* streamOut)` | 订阅事件，返回**事件流句柄**（非端口） |
| `int tailnet_next_event(int sh, int timeoutMS, char** out)` | 阻塞取一条事件 JSON；超时返回 `-ETIMEDOUT`，句柄无效返回 `EBADF` |
| `int tailnet_stop_watch(int sh)` | 取消订阅并销毁流句柄 |

## 最小 C 示例

```c
#include "tailnet.h"
#include <stdio.h>

int main(void) {
    int sd = tailnet_new();
    tailnet_configure(sd, "{\"dir\":\"C:\\\\tmp\\\\tailnet\",\"hostname\":\"c-demo\"}");
    if (tailnet_start(sd) != 0) {
        char err[512]; tailnet_errmsg(sd, err, sizeof err);
        fprintf(stderr, "start: %s\n", err);
        return 1;
    }

    /* 状态：返回 malloc 的 JSON，用完交给 tailnet_free。
       注意一次成功调用会清空 lastErr，所以要紧接着判返回值。 */
    char *json = NULL;
    if (tailnet_status_json(sd, &json) == 0) {
        puts(json);
        tailnet_free(json);
    }

    /* 隧道：拿到桥接端口后，用普通的 TCP 客户端连 127.0.0.1:port。
       timeoutMS <= 0 表示使用默认 30s。 */
    int port = 0;
    if (tailnet_dial(sd, "tcp", "backend.tail-scale.ts.net:443", 10000, &port) == 0) {
        char remote[128];
        if (tailnet_conn_remote_addr(port, remote, sizeof remote) == 0)
            printf("real peer: %s\n", remote);   /* 桥的宿主侧看到的是 127.0.0.1 */
    }

    /* 事件流：句柄 + 阻塞取；单线程轮询，超时返回 -ETIMEDOUT。 */
    int stream = 0;
    if (tailnet_watch(sd, &stream) == 0) {
        char *ev = NULL;
        int rc = tailnet_next_event(stream, 5000, &ev);
        if (rc == 0) { puts(ev); tailnet_free(ev); }
        tailnet_stop_watch(stream);
    }

    tailnet_close(sd);   /* 只释放句柄；malloc 的字符串用 tailnet_free */
    return 0;
}
```

多语言封装见 `bind/dotnet/TailnetSdk.cs`（C#）、`docs/API.md`（Go/概念）与 M4 的 Swift 封装。

