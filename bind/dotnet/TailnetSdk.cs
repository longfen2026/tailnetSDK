// Copyright (c) tailnetSDK contributors
// SPDX-License-Identifier: BSD-3-Clause

// TailnetSdk is a thin, dependency-free P/Invoke wrapper over libtailnet.dll
// (built from bind/ffi with `go build -buildmode=c-shared`). It targets .NET 8
// but has no NuGet dependencies beyond the standard library.
//
// Quick start:
//
//   var node = new TailnetNode(new TailnetOptions {
//       Dir = @"C:\ProgramData\MyApp\tailnet",
//       Hostname = "myapp-win",
//       EnableProxy = true,
//   });
//   await node.StartAsync();
//   var url = await node.GetLoginUrlAsync();   // open in the system browser
//   await node.WaitForRunningAsync();
//   var (addr, cred) = node.ProxyAddrs();      // loopback SOCKS5 for your stacks
//
// Tunnel traffic: DialAsync returns a TcpClient bridged into the tailnet, or
// point any SOCKS5-aware library at ProxyAddrs().

using System;
using System.Buffers;
using System.Net.Sockets;
using System.Runtime.InteropServices;
using System.Text;
using System.Text.Json;
using System.Threading;
using System.Threading.Tasks;

namespace Tailnet;

internal static unsafe class Native
{
    private const string Lib = "tailnet";

    // --- lifecycle ---------------------------------------------------------
    [DllImport(Lib, EntryPoint = "tailnet_new")] internal static extern int New();
    [DllImport(Lib, EntryPoint = "tailnet_configure")] internal static extern int Configure(int sd, byte[] cfg);
    [DllImport(Lib, EntryPoint = "tailnet_start")] internal static extern int Start(int sd);
    [DllImport(Lib, EntryPoint = "tailnet_up")] internal static extern int Up(int sd, int timeoutMs);
    [DllImport(Lib, EntryPoint = "tailnet_close")] internal static extern int Close(int sd);

    // --- status / login / session -----------------------------------------
    [DllImport(Lib, EntryPoint = "tailnet_status_json")] internal static extern int StatusJson(int sd, out IntPtr json);
    [DllImport(Lib, EntryPoint = "tailnet_peers_json")] internal static extern int PeersJson(int sd, out IntPtr json);
    [DllImport(Lib, EntryPoint = "tailnet_wait_running")] internal static extern int WaitForRunning(int sd, int timeoutMs, out IntPtr json);
    [DllImport(Lib, EntryPoint = "tailnet_login_url")] internal static extern int LoginUrl(int sd, byte[] buf, nuint size);
    [DllImport(Lib, EntryPoint = "tailnet_start_login_interactive")] internal static extern int StartLoginInteractive(int sd);
    [DllImport(Lib, EntryPoint = "tailnet_logout")] internal static extern int Logout(int sd);
    [DllImport(Lib, EntryPoint = "tailnet_profiles_json")] internal static extern int ProfilesJson(int sd, out IntPtr json);
    [DllImport(Lib, EntryPoint = "tailnet_switch_profile")] internal static extern int SwitchProfile(int sd, byte[] id);
    [DllImport(Lib, EntryPoint = "tailnet_delete_profile")] internal static extern int DeleteProfile(int sd, byte[] id);

    // --- tunnel ------------------------------------------------------------
    [DllImport(Lib, EntryPoint = "tailnet_dial")] internal static extern int Dial(int sd, byte[] network, byte[] addr, int timeoutMs, out int port);
    [DllImport(Lib, EntryPoint = "tailnet_listen")] internal static extern int Listen(int sd, byte[] network, byte[] addr, out int listener);
    [DllImport(Lib, EntryPoint = "tailnet_accept")] internal static extern int Accept(int listener, out int port);
    [DllImport(Lib, EntryPoint = "tailnet_listener_close")] internal static extern int ListenerClose(int listener);

    // --- proxy / diagnostics ------------------------------------------------
    [DllImport(Lib, EntryPoint = "tailnet_proxy_addrs")] internal static extern int ProxyAddrs(int sd, byte[] addr, nuint addrSize, byte[] cred, nuint credSize);
    [DllImport(Lib, EntryPoint = "tailnet_getips")] internal static extern int GetIps(int sd, byte[] v4, nuint v4Size, byte[] v6, nuint v6Size);
    [DllImport(Lib, EntryPoint = "tailnet_ping_json")] internal static extern int PingJson(int sd, byte[] addr, out IntPtr json);
    [DllImport(Lib, EntryPoint = "tailnet_whois_json")] internal static extern int WhoisJson(int sd, byte[] remoteAddr, out IntPtr json);
    [DllImport(Lib, EntryPoint = "tailnet_set_exit_node")] internal static extern int SetExitNode(int sd, byte[] nodeId);
    [DllImport(Lib, EntryPoint = "tailnet_clear_exit_node")] internal static extern int ClearExitNode(int sd);

    // --- events --------------------------------------------------------------
    [DllImport(Lib, EntryPoint = "tailnet_watch")] internal static extern int Watch(int sd, out int stream);
    [DllImport(Lib, EntryPoint = "tailnet_next_event")] internal static extern int NextEvent(int stream, int timeoutMs, out IntPtr json);
    [DllImport(Lib, EntryPoint = "tailnet_stop_watch")] internal static extern int StopWatch(int stream);

    // --- misc -----------------------------------------------------------------
    [DllImport(Lib, EntryPoint = "tailnet_errmsg")] internal static extern int ErrMsg(int sd, byte[] buf, nuint size);
    [DllImport(Lib, EntryPoint = "tailnet_version")] internal static extern int Version(byte[] buf, nuint size);
    [DllImport(Lib, EntryPoint = "tailnet_free")] internal static extern void Free(IntPtr p);
}

/// <summary>
/// One embedded tailnet node. Create with <see cref="TailnetNode"/>, which owns
/// the native handle; every instance must be disposed exactly once.
/// </summary>
public sealed class TailnetNode : IDisposable
{
    private int _sd; // native handle; -1 once disposed
    private readonly string _dir;

    public TailnetNode(TailnetOptions opt)
    {
        ArgumentNullException.ThrowIfNull(opt);
        if (string.IsNullOrWhiteSpace(opt.Dir))
            throw new ArgumentException("Dir is required (mobile/desktop apps must own a writable state directory).", nameof(opt));
        _dir = opt.Dir;
        _sd = Native.New();
        if (_sd <= 0)
            throw new TailnetException("tailnet_new failed");
        int rc = Native.Configure(_sd, JsonBytes(new
        {
            dir = opt.Dir,
            hostname = opt.Hostname,
            controlURL = opt.ControlURL,
            ephemeral = opt.Ephemeral,
            authKey = opt.AuthKey,
            advertiseTags = opt.AdvertiseTags,
            enableProxy = opt.EnableProxy,
        }));
        if (rc != 0)
        {
            string msg = LastError();
            Native.Close(_sd);
            _sd = -1;
            throw new TailnetException($"tailnet_configure failed ({rc}): {msg}");
        }
    }

    /// <summary>Starts the node without waiting for authorization.</summary>
    public Task StartAsync(CancellationToken ct = default) => Run(() => Check(Native.Start(_sd), "start"), ct);

    /// <summary>
    /// Starts the node and waits up to <paramref name="timeout"/> for it to
    /// become Running (authorized and usable). Returns the full status.
    /// </summary>
    public async Task<TailnetStatus> StartAndWaitAsync(TimeSpan? timeout = null, CancellationToken ct = default)
    {
        await StartAsync(ct).ConfigureAwait(false);
        return await WaitForRunningAsync(timeout, ct).ConfigureAwait(false);
    }

    /// <summary>Waits for the node to become Running. Returns the full status.</summary>
    public Task<TailnetStatus> WaitForRunningAsync(TimeSpan? timeout = null, CancellationToken ct = default) =>
        Run(() =>
        {
            int ms = TimeoutMs(timeout);
            IntPtr json = IntPtr.Zero;
            Check(Native.WaitForRunning(_sd, ms, out json), "wait_for_running");
            return FromJson<TailnetStatus>(json);
        }, ct);

    /// <summary>Full status snapshot, including the device list.</summary>
    public Task<TailnetStatus> GetStatusAsync(CancellationToken ct = default) =>
        Run(() => FromJson<TailnetStatus>(Native.StatusJson(_sd, out IntPtr json) == 0 ? json : throw Fail("status")), ct);

    /// <summary>Just the device list of the tailnet.</summary>
    public Task<TailnetPeer[]> GetPeersAsync(CancellationToken ct = default) =>
        Run(() => FromJson<TailnetPeer[]>(Native.PeersJson(_sd, out IntPtr json) == 0 ? json : throw Fail("peers")), ct);

    /// <summary>
    /// Returns the URL the user must open in the system browser to authorize
    /// this node, triggering interactive login first if necessary.
    /// </summary>
    public Task<string> GetLoginUrlAsync(CancellationToken ct = default) =>
        Run(() =>
        {
            byte[] buf = new byte[1024];
            int rc = Native.LoginUrl(_sd, buf, (nuint)buf.Length);
            if (rc == 0) return NullTerminated(buf);
            if (rc == unchecked((int)0x40001B)) // EMSGSIZE
            {
                buf = new byte[8192];
                Check(Native.LoginUrl(_sd, buf, (nuint)buf.Length), "login_url");
                return NullTerminated(buf);
            }
            throw Fail("login_url");
        }, ct);

    /// <summary>Logs the node out (deauthorizes it; a fresh login is required).</summary>
    public Task LogoutAsync(CancellationToken ct = default) => Run(() => Check(Native.Logout(_sd), "logout"), ct);

    /// <summary>Stored login profiles; item0 of the tuple is the active one.</summary>
    public Task<(TailnetProfile Current, TailnetProfile[] All)> GetProfilesAsync(CancellationToken ct = default) =>
        Run(() =>
        {
            IntPtr json = IntPtr.Zero;
            Check(Native.ProfilesJson(_sd, out json), "profiles");
            using JsonDocument doc = JsonDocument.Parse(FromNative(json));
            JsonElement root = doc.RootElement;
            TailnetProfile current = root.GetProperty("current").Deserialize<TailnetProfile>(JsonOpts) ?? new();
            TailnetProfile[] all = root.GetProperty("all").Deserialize<TailnetProfile[]>(JsonOpts) ?? Array.Empty<TailnetProfile>();
            return (current, all);
        }, ct);

    /// <summary>Activates a previously stored login profile by ID.</summary>
    public Task SwitchProfileAsync(string profileId, CancellationToken ct = default) =>
        Run(() => Check(Native.SwitchProfile(_sd, ZBytes(profileId)), "switch_profile"), ct);

    /// <summary>Deletes a stored login profile by ID.</summary>
    public Task DeleteProfileAsync(string profileId, CancellationToken ct = default) =>
        Run(() => Check(Native.DeleteProfile(_sd, ZBytes(profileId)), "delete_profile"), ct);

    public void Dispose()
    {
        int sd = Interlocked.Exchange(ref _sd, -1);
        if (sd > 0) _ = Native.Close(sd);
        GC.SuppressFinalize(this);
    }
    ~TailnetNode() => Dispose();

    // --- tunnel --------------------------------------------------------------

    /// <summary>
    /// Opens a TCP connection through the tailnet to <paramref name="addr"/>
    /// (MagicDNS name, hostname or tailnet IP with port). The returned
    /// <see cref="TcpClient"/> is bridged to a loopback listener inside the
    /// native library, so normal socket code works unchanged.
    /// </summary>
    public Task<TcpClient> DialAsync(string addr, TimeSpan? timeout = null, CancellationToken ct = default) =>
        Run(() =>
        {
            int ms = TimeoutMs(timeout);
            Check(Native.Dial(_sd, ZBytes("tcp"), ZBytes(addr), ms, out int port), "dial " + addr);
            var tcp = new TcpClient();
            try { tcp.Connect("127.0.0.1", port); }
            catch { tcp.Dispose(); throw; }
            return tcp;
        }, ct);

    /// <summary>
    /// Listens for tailnet connections. AcceptAsync returns bridged TcpClients.
    /// </summary>
    public Task<TailnetListener> ListenAsync(string addr, CancellationToken ct = default) =>
        Run(() =>
        {
            Check(Native.Listen(_sd, ZBytes("tcp"), ZBytes(addr), out int ln), "listen " + addr);
            return new TailnetListener(ln);
        }, ct);

    // --- proxy / diagnostics ---------------------------------------------------

    /// <summary>
    /// Starts (or reports) the loopback SOCKS5/HTTP proxy. Point any
    /// SOCKS5-aware stack (HttpClientHandler, curl, libraries...) at it.
    /// </summary>
    public (string Addr, string Password) ProxyAddrs()
    {
        byte[] addr = new byte[256], cred = new byte[256];
        Check(Native.ProxyAddrs(_sd, addr, (nuint)addr.Length, cred, (nuint)cred.Length), "proxy_addrs");
        return (NullTerminated(addr), NullTerminated(cred));
    }

    /// <summary>The node's tailnet IPv4 and IPv6 addresses.</summary>
    public (string V4, string V6) GetIps()
    {
        byte[] v4 = new byte[64], v6 = new byte[128];
        Check(Native.GetIps(_sd, v4, (nuint)v4.Length, v6, (nuint)v6.Length), "getips");
        return (NullTerminated(v4), NullTerminated(v6));
    }

    /// <summary>Connectivity diagnostic (equivalent of `tailscale ping`).</summary>
    public Task<TailnetPing> PingAsync(string tailnetIP, CancellationToken ct = default) =>
        Run(() => FromJson<TailnetPing>(Native.PingJson(_sd, ZBytes(tailnetIP), out IntPtr json) == 0 ? json : throw Fail("ping")), ct);

    /// <summary>Resolves the tailnet identity behind an accepted connection's remote address.</summary>
    public Task<TailnetIdentity> WhoIsAsync(string remoteAddr, CancellationToken ct = default) =>
        Run(() => FromJson<TailnetIdentity>(Native.WhoisJson(_sd, ZBytes(remoteAddr), out IntPtr json) == 0 ? json : throw Fail("whois")), ct);

    /// <summary>Routes outbound non-tailnet traffic through an exit node (Peer.ID).</summary>
    public Task SetExitNodeAsync(string nodeId, CancellationToken ct = default) =>
        Run(() => Check(Native.SetExitNode(_sd, ZBytes(nodeId)), "set_exit_node"), ct);

    /// <summary>Stops using an exit node.</summary>
    public Task ClearExitNodeAsync(CancellationToken ct = default) =>
        Run(() => Check(Native.ClearExitNode(_sd), "clear_exit_node"), ct);

    // --- plumbing --------------------------------------------------------------

    internal static readonly JsonSerializerOptions JsonOpts = new(JsonSerializerDefaults.Web);

    private static byte[] JsonBytes(object o) => Encoding.UTF8.GetBytes(JsonSerializer.Serialize(o, JsonOpts));

    private static byte[] ZBytes(string s) => Encoding.UTF8.GetBytes(s ?? string.Empty);

    private static string NullTerminated(byte[] buf)
    {
        int end = Array.IndexOf(buf, (byte)0);
        return end < 0 ? Encoding.UTF8.GetString(buf) : Encoding.UTF8.GetString(buf, 0, end);
    }

    internal static T FromJson<T>(IntPtr p)
    {
        string s = FromNative(p);
        return JsonSerializer.Deserialize<T>(s, JsonOpts)
               ?? throw new TailnetException("unexpected null JSON payload");
    }

    internal static string FromNative(IntPtr p)
    {
        if (p == IntPtr.Zero) return string.Empty;
        int len = 0;
        while (Marshal.ReadByte(p, len) != 0) len++;
        byte[] buf = new byte[len];
        Marshal.Copy(p, buf, 0, len);
        Native.Free(p);
        return Encoding.UTF8.GetString(buf);
    }

    private static int TimeoutMs(TimeSpan? t) => (int)(t ?? TimeSpan.FromMinutes(5)).TotalMilliseconds;

    private void Check(int rc, string op)
    {
        if (rc != 0)
        {
            string msg = LastError();
            throw new TailnetException($"{op} failed (rc={rc}): {msg}");
        }
    }

    private TailnetException Fail(string op) => new($"{op} failed: {LastError()}");

    private string LastError()
    {
        int sd = _sd;
        if (sd <= 0) return "node disposed";
        byte[] buf = new byte[2048];
        _ = Native.ErrMsg(sd, buf, (nuint)buf.Length);
        return NullTerminated(buf);
    }

    private Task<T> Run<T>(Func<T> op, CancellationToken ct) => Task.Run(op, ct);
    private Task Run(Action op, CancellationToken ct) => Task.Run(op, ct);
}

/// <summary>A listener that accepts connections arriving over the tailnet.</summary>
public sealed class TailnetListener : IDisposable
{
    private int _ln;
    internal TailnetListener(int ln) => _ln = ln;

    /// <summary>Accepts the next tailnet connection. Returns a bridged TcpClient.</summary>
    public Task<TcpClient> AcceptAsync(CancellationToken ct = default) => Task.Run(() =>
    {
        int ln = _ln;
        if (ln <= 0) throw new ObjectDisposedException(nameof(TailnetListener));
        int rc = Native.Accept(ln, out int port);
        if (rc != 0) throw new TailnetException($"accept failed (rc={rc})");
        var tcp = new TcpClient();
        try { tcp.Connect("127.0.0.1", port); }
        catch { tcp.Dispose(); throw; }
        return tcp;
    }, ct);

    public void Dispose()
    {
        int ln = Interlocked.Exchange(ref _ln, -1);
        if (ln > 0) _ = Native.ListenerClose(ln);
        GC.SuppressFinalize(this);
    }
    ~TailnetListener() => Dispose();
}

/// <summary>A live event stream from the node.</summary>
public sealed class TailnetEventStream : IDisposable
{
    private int _stream;
    internal TailnetEventStream(int stream) => _stream = stream;

    /// <summary>
    /// Waits for the next event (up to <paramref name="timeout"/>). Returns
    /// null when the timeout elapses without an event.
    /// </summary>
    public Task<TailnetEvent?> NextAsync(TimeSpan? timeout = null, CancellationToken ct = default) => Task.Run(() =>
    {
        int stream = _stream;
        if (stream <= 0) throw new ObjectDisposedException(nameof(TailnetEventStream));
        IntPtr json = IntPtr.Zero;
        int rc = Native.NextEvent(stream, (int)(timeout ?? TimeSpan.FromSeconds(30)).TotalMilliseconds, out json);
        if (rc == -110) return null; // -ETIMEDOUT: no event within the window
        if (rc != 0) throw new TailnetException($"next_event failed (rc={rc})");
        string s = TailnetNode.FromNative(json);
        return JsonSerializer.Deserialize<TailnetEvent>(s, TailnetNode.JsonOpts);
    }, ct);

    public void Dispose()
    {
        int stream = Interlocked.Exchange(ref _stream, -1);
        if (stream > 0) _ = Native.StopWatch(stream);
        GC.SuppressFinalize(this);
    }
    ~TailnetEventStream() => Dispose();
}

/// <summary>Options for creating a <see cref="TailnetNode"/>.</summary>
public sealed class TailnetOptions
{
    /// <summary>State directory (REQUIRED). The node key is stored here, so it
    /// must be private and App-writable.</summary>
    public string Dir { get; set; } = string.Empty;
    /// <summary>Device name inside the tailnet (default "tailnetsdk").</summary>
    public string? Hostname { get; set; }
    /// <summary>Coordination server override. Empty = official Tailscale.</summary>
    public string? ControlURL { get; set; }
    /// <summary>Ephemeral node: disappears from the tailnet when disconnected.</summary>
    public bool Ephemeral { get; set; }
    /// <summary>Auth key for unattended login. Empty = interactive browser login.</summary>
    public string? AuthKey { get; set; }
    /// <summary>ACL tags requested for this node.</summary>
    public string[]? AdvertiseTags { get; set; }
    /// <summary>Start the loopback SOCKS5/HTTP proxy at start.</summary>
    public bool EnableProxy { get; set; }
}

/// <summary>Errors thrown by <see cref="TailnetNode"/>; Message includes the
/// native operation and the node's last error text.</summary>
public sealed class TailnetException : Exception
{
    public TailnetException(string message) : base(message) { }
}

// ---- DTOs (mirror core/dto.go JSON wire format) -----------------------------

/// <summary>Backend state string, e.g. "Running", "NeedsLogin".</summary>
public static class TailnetState
{
    public const string Running = "Running";
    public const string NeedsLogin = "NeedsLogin";
    public const string Starting = "Starting";
    public const string Stopped = "Stopped";
    public const string NeedsMachineAuth = "NeedsMachineAuth";
}

public sealed class TailnetStatus
{
    public string State { get; set; } = string.Empty;
    public string? AuthURL { get; set; }
    public string? Version { get; set; }
    public bool Tun { get; set; }
    public string? TailnetName { get; set; }
    public string? MagicDNSSuffix { get; set; }
    public bool MagicDNS { get; set; }
    public string[]? TailscaleIPs { get; set; }
    public TailnetPeer? Self { get; set; }
    public TailnetPeer[]? Peers { get; set; }
    public string[]? Health { get; set; }
    public bool IsRunning() => State == TailnetState.Running;
}

public sealed class TailnetPeer
{
    public string? ID { get; set; }
    public string? NodeID { get; set; }
    public string? HostName { get; set; }
    public string? DNSName { get; set; }
    public string? OS { get; set; }
    public string? UserID { get; set; }
    public string[]? IPs { get; set; }
    public string[]? Tags { get; set; }
    public bool Online { get; set; }
    public bool Active { get; set; }
    public bool Expired { get; set; }
    public DateTimeOffset? LastSeen { get; set; }
    public DateTimeOffset? KeyExpiry { get; set; }
}

public sealed class TailnetProfile
{
    public string? ID { get; set; }
    public string? Name { get; set; }
    public string? LoginName { get; set; }
    public string? DisplayName { get; set; }
    public string? NodeID { get; set; }
    public string? ControlURL { get; set; }
}

public sealed class TailnetIdentity
{
    public string? UserID { get; set; }
    public string? LoginName { get; set; }
    public string? DisplayName { get; set; }
    public string? NodeID { get; set; }
    public string? NodeName { get; set; }
    public string[]? Tags { get; set; }
}

public sealed class TailnetPing
{
    public string? IP { get; set; }
    public string? NodeIP { get; set; }
    public string? NodeName { get; set; }
    public double LatencySeconds { get; set; }
    public string? Endpoint { get; set; }
    public int DERPRegionID { get; set; }
    public string? DERPRegionCode { get; set; }
    public string? Err { get; set; }
}

public sealed class TailnetEvent
{
    public string Kind { get; set; } = string.Empty;
    public DateTimeOffset Time { get; set; }
    public string? State { get; set; }
    public string? AuthURL { get; set; }
    public string[]? Health { get; set; }
    public TailnetPeer? Self { get; set; }
    public int PeerCount { get; set; }
    public string? Message { get; set; }
}


