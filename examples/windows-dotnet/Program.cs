// Copyright (c) tailnetSDK contributors
// SPDX-License-Identifier: BSD-3-Clause
//
// tailnet-demo shows the full C# integration of the embedded tailnet node on
// Windows: start the node, log in through the system browser, list devices and
// open a tailnet tunnel.
//
//   dotnet run -- --dir %LOCALAPPDATA%\tailnet-demo --hostname demo-win
//
// Modes:
//   default      interactive login (opens the login URL) and stays connected
//   --smoke      no-account check: start, print status, exit
//   --logout     deauthorize the node in --dir and exit
//   --proxy      also print the loopback SOCKS5/HTTP proxy address

using System.Diagnostics;
using System.Net.Sockets;
using System.Text;
using Tailnet;

string dir = GetArg("--dir") ?? Path.Combine(Path.GetTempPath(), "tailnet-demo");
string hostname = GetArg("--hostname") ?? "demo-win";
string? controlURL = GetArg("--control-url");
string? authKey = GetArg("--authkey");
bool smoke = HasFlag("--smoke");
bool logout = HasFlag("--logout");
bool proxy = HasFlag("--proxy") || HasFlag("--enable-proxy");
int staySeconds = int.TryParse(GetArg("--stay"), out int s) ? s : 0;

using var node = new TailnetNode(new TailnetOptions
{
    Dir = dir,
    Hostname = hostname,
    ControlURL = controlURL,
    AuthKey = authKey,
    Ephemeral = HasFlag("--ephemeral"),
    EnableProxy = proxy,
});

if (logout)
{
    await node.StartAsync();
    await node.LogoutAsync();
    Console.WriteLine("logged out; a fresh login is required next time.");
    return 0;
}

await node.StartAsync();

TailnetStatus st = await node.GetStatusAsync();
Console.WriteLine($"backend state: {st.State} (client {st.Version})");

if (!st.IsRunning())
{
    // --smoke is a pure offline check of the managed -> native chain: it must
    // not touch the control plane, so it stops before fetching a login URL.
    if (smoke)
    {
        Console.WriteLine("--smoke: not authorized; skipping login URL and interactive wait.");
        return 0;
    }
    string url = await node.GetLoginUrlAsync();
    Console.WriteLine();
    Console.WriteLine("Open this URL in your browser to authorize this device:");
    Console.WriteLine($"  {url}");
    // Prefer the system browser; falls back to printing the URL above.
    try { Process.Start(new ProcessStartInfo(url) { UseShellExecute = true }); }
    catch { /* headless or blocked: the printed URL still works */ }

    Console.Write("waiting for authorization");
    st = await node.WaitForRunningAsync(TimeSpan.FromMinutes(5));
    Console.WriteLine($" -> {st.State}");
    if (!st.IsRunning())
    {
        Console.Error.WriteLine("node did not reach Running within 5 minutes.");
        return 1;
    }
}
else if (smoke)
{
    return 0;
}

// Connected: show identity, device list and (optionally) the local proxy.
var (v4, v6) = node.GetIps();
Console.WriteLine();
Console.WriteLine($"tailnet : {st.TailnetName}");
Console.WriteLine($"this node: {st.Self?.HostName} {v4} {v6}");

// Freshly restored nodes reach Running before the control plane pushes the
// peer list; poll briefly so device listing and tunneling see a real netmap.
for (int i = 0; i < 40 && (st.Peers?.Length ?? 0) == 0; i++)
{
    await Task.Delay(500);
    st = await node.GetStatusAsync();
}

Console.WriteLine($"devices  : {st.Peers?.Length ?? 0}");
foreach (TailnetPeer p in st.Peers ?? Array.Empty<TailnetPeer>())
{
    string ips = p.IPs is { Length: > 0 } ? string.Join(',', p.IPs) : "-";
    Console.WriteLine($"  - {p.HostName,-24} {p.OS,-10} {(p.Online ? "online " : "offline")} {ips}");
}

if (proxy)
{
    (string addr, string cred) = node.ProxyAddrs();
    Console.WriteLine();
    Console.WriteLine($"loopback proxy: socks5://tsnet:{cred}@{addr} (also serves HTTP CONNECT)");
    Console.WriteLine("point any SOCKS5-aware HTTP stack at it to route through the tailnet.");
}

// Optional tunnel verification against a real tailnet device:
//   --ping 100.115.115.115          TSMP ping (connectivity diagnostic)
//   --dial 100.115.115.115:22       open a tailnet TCP connection, print any banner
if (GetArg("--ping") is string pingTarget)
{
    TailnetPing ping = await node.PingAsync(pingTarget);
    Console.WriteLine();
    Console.WriteLine($"ping {pingTarget}: err={ping.Err ?? "none"} latency={ping.LatencySeconds}s via={ping.Endpoint ?? ping.DERPRegionCode ?? "direct"}");
}

if (GetArg("--dial") is string dialTargets)
{
    foreach (string dialTarget in dialTargets.Split(',', StringSplitOptions.RemoveEmptyEntries | StringSplitOptions.TrimEntries))
    {
        Console.WriteLine();
        Console.WriteLine($"dialing {dialTarget} over the tailnet...");
        try
        {
            using TcpClient tcp = await node.DialAsync(dialTarget, TimeSpan.FromSeconds(10));
            tcp.ReceiveTimeout = 5000;
            Console.WriteLine($"connected: local={tcp.Client.LocalEndPoint} remote={tcp.Client.RemoteEndPoint}");
            try
            {
                var buf = new byte[256];
                int n = await tcp.Client.ReceiveAsync(buf);
                Console.WriteLine(n > 0
                    ? $"banner ({n} bytes): {Encoding.UTF8.GetString(buf, 0, n).TrimEnd()}"
                    : "connected; peer sent no banner (normal for HTTP/HTTP CONNECT endpoints)");
            }
            catch (Exception ex) when (ex is SocketException or IOException)
            {
                Console.WriteLine("connected; no banner within 5s (peer waiting for our request).");
            }
        }
        catch (Exception ex)
        {
            Console.WriteLine($"dial failed: {ex.Message}");
        }
    }
}

// --serve <port>: expose a tiny HTTP service to the tailnet. Visit it from
// any other tailnet device at http://<this node's tailnet IP>:<port>.
if (GetArg("--serve") is string servePort)
{
    TailnetListener ln = await node.ListenAsync(":" + servePort);
    Console.WriteLine();
    Console.WriteLine($"serving HTTP on tailnet :{servePort} — visit http://{v4}:{servePort} from another tailnet device");
    _ = Task.Run(async () =>
    {
        while (true)
        {
            TailnetBridgedConnection conn;
            try { conn = await ln.AcceptAsync(); }
            catch { return; }
            _ = Task.Run(async () =>
            {
                using TailnetBridgedConnection cc = conn;
                string remote = cc.RemoteTailnetEndPoint != ""
                    ? cc.RemoteTailnetEndPoint
                    : cc.Client.Client.RemoteEndPoint?.ToString() ?? "";
                var buf = new byte[4096];
                int n;
                try { n = await cc.Client.Client.ReceiveAsync(buf); }
                catch { return; }
                string requestLine = n > 0
                    ? Encoding.UTF8.GetString(buf, 0, n).Split("\r\n", 2)[0]
                    : "(empty request)";
                string who = "";
                try
                {
                    TailnetIdentity id = await node.WhoIsAsync(remote);
                    who = id.LoginName ?? id.NodeName ?? "";
                }
                catch { /* whois is best-effort */ }
                Console.WriteLine($"  <- {remote} \"{requestLine}\" (identity: {(who == "" ? "n/a" : who)})");
                string body = $"hello from demo-win over the tailnet! your identity: {who}\n";
                byte[] resp = Encoding.UTF8.GetBytes(
                    "HTTP/1.1 200 OK\r\nContent-Type: text/plain\r\nConnection: close\r\n" +
                    $"Content-Length: {Encoding.UTF8.GetByteCount(body)}\r\n\r\n{body}");
                try { await cc.Client.Client.SendAsync(resp); }
                catch { /* client hung up */ }
            });
        }
    });
}

if (staySeconds > 0)
{
    Console.WriteLine($"staying connected for {staySeconds}s...");
    await Task.Delay(TimeSpan.FromSeconds(staySeconds));
}

return 0;

// ---- arg helpers ------------------------------------------------------------

static string? GetArg(string name)
{
    string[] a = Environment.GetCommandLineArgs();
    for (int i = 0; i < a.Length - 1; i++)
        if (a[i].Equals(name, StringComparison.OrdinalIgnoreCase))
            return a[i + 1];
    return null;
}

static bool HasFlag(string name) =>
    Environment.GetCommandLineArgs().Any(x => x.Equals(name, StringComparison.OrdinalIgnoreCase));
