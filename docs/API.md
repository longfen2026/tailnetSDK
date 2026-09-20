# SDK API 参考（M0）

包路径：`tailnetsdk/core`

## 1. 创建与生命周期

```go
type Config struct {
    Dir           string   // 状态目录；移动端必须显式指定（App 沙盒路径）
    Hostname      string   // 设备名，显示在 tailnet 管理后台（默认 "tailnetsdk"）
    ControlURL    string   // 协调服务器，空 = 官方 controlplane.tailscale.com
    Ephemeral     bool     // 临时节点：断开即从 tailnet 移除
    AuthKey       string   // 非交互授权；留空则交互式登录
    ClientSecret  string   // OAuth 铸钥（需引入 tailscale.com/feature/oauthclient）
    AdvertiseTags []string // 申请的 ACL 标签
    EnableProxy   bool     // Start 时自动启动本地 SOCKS5/HTTP 代理
    Logf          func(format string, args ...any) // 调试日志
    UserLogf      func(format string, args ...any) // 面向用户的日志（含登录 URL）
}

func New(cfg Config) (*Node, error)
func (n *Node) Start(ctx context.Context) error      // 启动并加入 tailnet（不等待授权完成）
func (n *Node) Up(ctx context.Context) (*Status, error) // 启动并等待可用
func (n *Node) Close() error                          // 幂等
func (n *Node) Started() bool
func (n *Node) Config() Config
func (n *Node) RootPath() string
func (n *Node) Server() (*tsnet.Server, error)        // 逃生舱
func (n *Node) LocalClient() (*local.Client, error)   // 进程内 LocalAPI（iOS 挂起安全）
func (n *Node) LocalIPs() (netip.Addr, netip.Addr)
```

## 2. 登录授权

```go
func (n *Node) StartLoginInteractive(ctx context.Context) error
func (n *Node) LoginURL(ctx context.Context) (string, error)
func (n *Node) WaitForRunning(ctx context.Context) (*Status, error)
```

登录流程：

1. `Node.LoginURL` 返回形如 `https://login.tailscale.com/a/xxxx` 的 URL；
2. 宿主用**系统浏览器**打开（Android Custom Tabs / iOS `ASWebAuthenticationSession`）；
3. 用户在浏览器完成 IdP 登录后，节点通过控制连接自动拿到授权，**不需要回调 URI / deep link**；
4. `Node.Watch` 会先后推送 `auth_url`、`state`、`login_finished`、`self`、`peers` 事件；也可用 `WaitForRunning` 阻塞等待。

错误语义：

- `ErrCodeLoginRequired`：`NeedsMachineAuth`，需要管理员在后台批准设备；
- `ErrCodeTimeout`：等待授权/启动超时（ctx 取消）。

## 3. 状态查询

```go
func (n *Node) Status(ctx context.Context) (*Status, error)             // 含设备列表
func (n *Node) StatusWithoutPeers(ctx context.Context) (*Status, error) // 轮询用，更轻
func (n *Node) Peers(ctx context.Context) ([]Peer, error)
func (n *Node) Watch(ctx context.Context, fn func(Event)) (stop func(), err error)
```

`Status.State` 取值：`NoState` / `InUseOtherUser` / `NeedsLogin` / `NeedsMachineAuth` / `Stopped` / `Starting` / `Running`。
`Status.AuthURL` 只在需要登录时非空；`Status.Self` 是本设备身份；`Status.TailscaleIPs` 是 tailnet IP；
`Status.TUN` 恒为 `false`（用户态模式）。

`Event.Kind`：`auth_url` / `state` / `health` / `login_finished` / `self` / `peers` / `error`。
回调在独立 goroutine 上执行，不要阻塞。

## 4. 会话与设备

```go
func (n *Node) Logout(ctx context.Context) error
func (n *Node) ProfileStatus(ctx context.Context) (*Profile, []Profile, error)
func (n *Node) SwitchProfile(ctx context.Context, id string) error
func (n *Node) DeleteProfile(ctx context.Context, id string) error
```

注意：`Logout` 会清除本机会话并让节点失去授权，但**设备条目仍会留在管理后台**，需管理员删除。

## 5. 隧道

```go
func (n *Node) Dial(ctx context.Context, network, addr string) (net.Conn, error)
func (n *Node) Listen(network, addr string) (net.Listener, error)
func (n *Node) HTTPClient() (*http.Client, error)
func (n *Node) StartProxy() (addr, cred string, err error)
func (n *Node) ProxyAddrs() (addr, cred string, ok bool)
```

- `Dial` 支持 MagicDNS 名（`host.tail-scale.ts.net:443`）、裸主机名、tailnet IP；**只有经由此处建立的连接**才走 tailnet。
- `StartProxy` 返回的地址同时提供 SOCKS5（用户名 `tsnet`，密码为返回值）与 HTTP 代理，仅监听 127.0.0.1，供宿主 App 中非 Go 的网络栈使用。
- 代理也可通过 `Config.EnableProxy` 在 `Start` 时自动拉起。

## 6. 增强能力

```go
func (n *Node) SetExitNode(ctx context.Context, nodeID string) error // nodeID 来自 Peer.ID
func (n *Node) ClearExitNode(ctx context.Context) error
func (n *Node) Ping(ctx context.Context, addr string) (*PingResult, error)
func (n *Node) WhoIs(ctx context.Context, remoteAddr string) (*Identity, error)
```

## 7. 错误模型

所有错误都是 `*core.Error`，携带稳定的 `ErrorCode`（供 JNI/Swift/.NET 映射）：

```go
func Code(err error) ErrorCode
func IsCode(err error, code ErrorCode) bool
```

错误码：`invalid_argument`、`not_started`、`timeout`、`login_required`、`closed`、`backend`、`unknown`。
