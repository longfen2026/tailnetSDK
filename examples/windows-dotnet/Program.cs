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
    string url = await node.GetLoginUrlAsync();
    Console.WriteLine();
    Console.WriteLine("Open this URL in your browser to authorize this device:");
    Console.WriteLine($"  {url}");
    // Prefer the system browser; falls back to printing the URL above.
    try { Process.Start(new ProcessStartInfo(url) { UseShellExecute = true }); }
    catch { /* headless or blocked: the printed URL still works */ }

    if (smoke)
    {
        Console.WriteLine("--smoke: skipping the interactive wait.");
        return 0;
    }
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
