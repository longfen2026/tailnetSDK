// Command tailnetctl is the smoke-test and acceptance CLI for the tailnet SDK.
//
// It exercises every capability the SDK exposes - login, status, device list,
// logout, tailnet dial/listen and the local SOCKS5/HTTP proxy - against a real
// tailnet, using the official Tailscale control plane by default.
//
// Build:  go build ./cli/tailnetctl
// Usage:  tailnetctl help
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"tailnetsdk/core"
)

// options holds the flags shared by every subcommand.
type options struct {
	dir        string
	hostname   string
	authKey    string
	controlURL string
	ephemeral  bool
	proxy      bool
	verbose    bool
	asJSON     bool
	timeout    time.Duration
}

// command describes one subcommand.
type command struct {
	summary string
	usage   string
	extra   func(fs *flag.FlagSet)
	run     func(ctx context.Context, o *options, pos []string) error
}

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		usage()
		return
	}
	name, rest := args[0], args[1:]
	if name == "help" || name == "-h" || name == "--help" {
		usage()
		return
	}
	cmd, ok := commands[name]
	if !ok {
		fmt.Fprintf(os.Stderr, "tailnetctl: unknown command %q\n\n", name)
		usage()
		os.Exit(2)
	}

	o := &options{}
	fs := flag.NewFlagSet("tailnetctl "+name, flag.ExitOnError)
	fs.StringVar(&o.dir, "dir", defaultDir(), "state directory (defaults to the user config dir)")
	fs.StringVar(&o.hostname, "hostname", "", "device name in the tailnet (default: "+core.DefaultHostname+")")
	fs.StringVar(&o.authKey, "authkey", "", "auth key for unattended login; empty means interactive login")
	fs.StringVar(&o.controlURL, "control-url", "", "coordination server (default: "+core.DefaultControlURL+")")
	fs.BoolVar(&o.ephemeral, "ephemeral", false, "register as an ephemeral node")
	fs.BoolVar(&o.proxy, "proxy", false, "start the local SOCKS5/HTTP proxy")
	fs.BoolVar(&o.verbose, "v", false, "verbose SDK logging")
	fs.BoolVar(&o.asJSON, "json", false, "machine readable JSON output")
	fs.DurationVar(&o.timeout, "timeout", 2*time.Minute, "overall timeout (0 disables it)")
	if cmd.extra != nil {
		cmd.extra(fs)
	}
	if err := fs.Parse(rest); err != nil {
		os.Exit(2)
	}

	ctx, cancel := signalContext(o.timeout)
	defer cancel()
	if err := cmd.run(ctx, o, fs.Args()); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintf(os.Stdout, `tailnetctl %s - tailnet SDK smoke test

Usage:
  tailnetctl <command> [flags] [args]

Commands:
`, core.Version)
	names := make([]string, 0, len(commands))
	for name := range commands {
		names = append(names, name)
	}
	sortStrings(names)
	for _, name := range names {
		cmd := commands[name]
		fmt.Fprintf(os.Stdout, "  %-8s %s\n", name, cmd.summary)
		if cmd.usage != "" {
			fmt.Fprintf(os.Stdout, "           %s\n", cmd.usage)
		}
	}
	fmt.Fprint(os.Stdout, `
Flags (all commands):
  -dir string           state directory
  -hostname string      device name in the tailnet
  -authkey string       auth key for unattended login
  -control-url string   coordination server
  -ephemeral            register as an ephemeral node
  -proxy                start the local SOCKS5/HTTP proxy
  -json                 machine readable output
  -v                    verbose logs
  -timeout duration     overall timeout (default 2m, 0 disables)

Environment:
  TAILNETSDK_DIR        overrides the default state directory
`)
}

// newNode builds an SDK node from the CLI flags.
func newNode(o *options) (*core.Node, error) {
	cfg := core.Config{
		Dir:         o.dir,
		Hostname:    o.hostname,
		AuthKey:     o.authKey,
		ControlURL:  o.controlURL,
		Ephemeral:   o.ephemeral,
		EnableProxy: o.proxy,
		UserLogf: func(format string, args ...any) {
			fmt.Fprintf(os.Stderr, "[tsnet] "+format+"\n", args...)
		},
	}
	if o.verbose {
		cfg.Logf = func(format string, args ...any) {
			fmt.Fprintf(os.Stderr, "[debug] "+format+"\n", args...)
		}
	}
	return core.New(cfg)
}

func signalContext(timeout time.Duration) (context.Context, context.CancelFunc) {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	if timeout <= 0 {
		return ctx, stop
	}
	tctx, cancel := context.WithTimeout(ctx, timeout)
	return tctx, func() {
		cancel()
		stop()
	}
}

func defaultDir() string {
	if dir := os.Getenv("TAILNETSDK_DIR"); dir != "" {
		return dir
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return filepath.Join(".", "tailnetsdk-data")
	}
	return filepath.Join(base, "tailnetsdk", "cli")
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

func printJSON(v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(b))
	return nil
}

func printStatus(st *core.Status) {
	fmt.Printf("state:       %s\n", st.State)
	if st.AuthURL != "" {
		fmt.Printf("auth URL:    %s\n", st.AuthURL)
	}
	if st.Self != nil {
		fmt.Printf("device:      %s  (id %s)\n", st.Self.DNSName, st.Self.ID)
		fmt.Printf("os:          %s\n", st.Self.OS)
	}
	fmt.Printf("tailnet:     %s  (MagicDNS suffix %q, enabled=%v)\n", st.TailnetName, st.MagicDNSSuffix, st.MagicDNS)
	fmt.Printf("tailnet IPs: %s\n", strings.Join(st.TailscaleIPs, ", "))
	fmt.Printf("client:      %s  (tun=%v)\n", st.Version, st.TUN)
	fmt.Printf("peers:       %d (%d online)\n", len(st.Peers), st.OnlinePeerCount())
	if len(st.Health) > 0 {
		fmt.Printf("health:      %s\n", strings.Join(st.Health, "; "))
	}
}

func printPeers(peers []core.Peer) {
	fmt.Printf("%-28s %-8s %-16s %-7s %s\n", "DNS NAME", "OS", "IP", "ONLINE", "LAST SEEN")
	for _, p := range peers {
		ip := ""
		if len(p.IPs) > 0 {
			ip = p.IPs[0]
		}
		last := "-"
		if !p.LastSeen.IsZero() {
			last = p.LastSeen.Format(time.RFC3339)
		}
		fmt.Printf("%-28s %-8s %-16s %-7v %s\n", p.DNSName, p.OS, ip, p.Online, last)
	}
}

var (
	loginURLOnly bool
	dialMessage  string
)

var commands = map[string]*command{
	"login": {
		summary: "authorize this device and print the login URL",
		extra: func(fs *flag.FlagSet) {
			fs.BoolVar(&loginURLOnly, "url-only", false, "print the auth URL and exit without waiting for authorization")
		},
		run: cmdLogin,
	},
	"status": {
		summary: "print the authorization and connectivity status",
		run:     cmdStatus,
	},
	"peers": {
		summary: "list the devices in the tailnet",
		run:     cmdPeers,
	},
	"logout": {
		summary: "deauthorize this device and clear its session",
		run:     cmdLogout,
	},
	"watch": {
		summary: "stream backend events as JSON lines",
		run:     cmdWatch,
	},
	"dial": {
		summary: "open a TCP connection over the tailnet",
		usage:   "dial [-msg text] host:port",
		extra: func(fs *flag.FlagSet) {
			fs.StringVar(&dialMessage, "msg", "", "line to send before reading the reply")
		},
		run: cmdDial,
	},
	"listen": {
		summary: "accept tailnet connections and echo them back",
		usage:   "listen host:port",
		run:     cmdListen,
	},
	"proxy": {
		summary: "run the loopback SOCKS5/HTTP proxy onto the tailnet",
		run:     cmdProxy,
	},
	"version": {
		summary: "print the SDK version",
		run:     cmdVersion,
	},
}

func cmdLogin(ctx context.Context, o *options, _ []string) error {
	node, err := newNode(o)
	if err != nil {
		return err
	}
	defer node.Close()
	if err := node.Start(ctx); err != nil {
		return err
	}
	url, err := node.LoginURL(ctx)
	if err != nil {
		// An already authorized node has no auth URL: that is not a failure.
		if core.IsCode(err, core.ErrCodeBackend) {
			fmt.Fprintln(os.Stderr, "device is already authorized")
			return nil
		}
		return err
	}
	if o.asJSON {
		if err := printJSON(map[string]string{"authURL": url, "hostname": node.Config().Hostname}); err != nil {
			return err
		}
	} else {
		fmt.Printf("Open this URL in a browser to authorize %q:\n\n  %s\n\n", node.Config().Hostname, url)
	}
	if loginURLOnly {
		return nil
	}
	st, err := node.WaitForRunning(ctx)
	if err != nil {
		return err
	}
	if o.asJSON {
		return printJSON(st)
	}
	printStatus(st)
	return nil
}

func cmdStatus(ctx context.Context, o *options, _ []string) error {
	node, err := newNode(o)
	if err != nil {
		return err
	}
	defer node.Close()
	if err := node.Start(ctx); err != nil {
		return err
	}
	st, err := node.Status(ctx)
	if err != nil {
		return err
	}
	if o.asJSON {
		return printJSON(st)
	}
	printStatus(st)
	return nil
}

func cmdPeers(ctx context.Context, o *options, _ []string) error {
	node, err := newNode(o)
	if err != nil {
		return err
	}
	defer node.Close()
	if err := node.Start(ctx); err != nil {
		return err
	}
	peers, err := node.Peers(ctx)
	if err != nil {
		return err
	}
	if o.asJSON {
		return printJSON(peers)
	}
	printPeers(peers)
	return nil
}

func cmdLogout(ctx context.Context, o *options, _ []string) error {
	node, err := newNode(o)
	if err != nil {
		return err
	}
	defer node.Close()
	if err := node.Start(ctx); err != nil {
		return err
	}
	if err := node.Logout(ctx); err != nil {
		return err
	}
	st, err := node.StatusWithoutPeers(ctx)
	if err != nil {
		return err
	}
	if o.asJSON {
		return printJSON(st)
	}
	fmt.Printf("logged out; backend state is now %s\n", st.State)
	return nil
}

func cmdWatch(ctx context.Context, o *options, _ []string) error {
	node, err := newNode(o)
	if err != nil {
		return err
	}
	defer node.Close()
	if err := node.Start(ctx); err != nil {
		return err
	}
	stop, err := node.Watch(ctx, func(ev core.Event) {
		if b, err := json.Marshal(ev); err == nil {
			fmt.Println(string(b))
		}
	})
	if err != nil {
		return err
	}
	defer stop()
	<-ctx.Done()
	return nil
}

func cmdDial(ctx context.Context, o *options, pos []string) error {
	if len(pos) != 1 {
		return errors.New("usage: tailnetctl dial [-msg text] host:port")
	}
	addr := pos[0]
	node, err := newNode(o)
	if err != nil {
		return err
	}
	defer node.Close()
	if err := node.Start(ctx); err != nil {
		return err
	}
	conn, err := node.Dial(ctx, "tcp", addr)
	if err != nil {
		return err
	}
	defer conn.Close()
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	} else {
		_ = conn.SetDeadline(time.Now().Add(15 * time.Second))
	}

	msg := dialMessage
	if msg == "" {
		msg = "hello over tailnet"
	}
	if _, err := io.WriteString(conn, msg+"\n"); err != nil {
		return fmt.Errorf("write: %w", err)
	}
	reply, err := io.ReadAll(io.LimitReader(conn, 64<<10))
	if err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("read reply: %w", err)
	}
	fmt.Printf("connected to %s (local %v, remote %v)\n", addr, conn.LocalAddr(), conn.RemoteAddr())
	fmt.Printf("sent:  %s\n", msg)
	fmt.Printf("reply: %s\n", strings.TrimSpace(string(reply)))
	return nil
}

func cmdListen(ctx context.Context, o *options, pos []string) error {
	if len(pos) != 1 {
		return errors.New("usage: tailnetctl listen host:port")
	}
	node, err := newNode(o)
	if err != nil {
		return err
	}
	defer node.Close()
	if err := node.Start(ctx); err != nil {
		return err
	}
	ln, err := node.Listen("tcp", pos[0])
	if err != nil {
		return err
	}
	defer ln.Close()
	v4, v6 := node.LocalIPs()
	fmt.Printf("listening on %v (tailnet IPs: %v %v)\n", ln.Addr(), v4, v6)
	fmt.Printf("reach it from another node with: tailnetctl dial %v\n", ln.Addr())

	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()
	for {
		c, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		go echoConn(ctx, node, c)
	}
}

// echoConn prints the identity of the caller using the LocalAPI WhoIs lookup,
// then echoes everything it receives.
func echoConn(ctx context.Context, node *core.Node, c net.Conn) {
	defer c.Close()
	from := c.RemoteAddr().String()
	if id, err := node.WhoIs(ctx, from); err == nil {
		fmt.Printf("connection from %s (login %q, node %q, nodeID %s)\n", from, id.LoginName, id.NodeName, id.NodeID)
	} else {
		fmt.Printf("connection from %s (whois failed: %v)\n", from, err)
	}
	_, _ = io.Copy(c, c)
}

func cmdProxy(ctx context.Context, o *options, _ []string) error {
	o.proxy = true
	node, err := newNode(o)
	if err != nil {
		return err
	}
	defer node.Close()
	if err := node.Start(ctx); err != nil {
		return err
	}
	addr, cred, ok := node.ProxyAddrs()
	if !ok {
		addr, cred, err = node.StartProxy()
		if err != nil {
			return err
		}
	}
	fmt.Printf("local tailnet proxy: %s\n", addr)
	fmt.Printf("  SOCKS5 : socks5h://tsnet:%s@%s\n", cred, addr)
	fmt.Printf("  HTTP   : http://tsnet:%s@%s\n", cred, addr)
	fmt.Printf("  example: curl --proxy socks5h://tsnet:%s@%s http://<peer>/\n", cred, addr)
	<-ctx.Done()
	return nil
}

func cmdVersion(_ context.Context, _ *options, _ []string) error {
	fmt.Printf("tailnetctl (tailnetsdk) %s\n", core.Version)
	fmt.Printf("control plane: %s\n", core.DefaultControlURL)
	return nil
}
