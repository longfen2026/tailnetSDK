// Command quickstart is a minimal host application built on the tailnet SDK.
//
//	go run ./examples/quickstart -hostname my-app
//
// It authorizes the node (printing the login URL), waits for the node to come
// up and prints the tailnet device list - the three capabilities every host app
// needs: login, status and device discovery.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"time"

	"tailnetsdk/core"
)

func main() {
	hostname := flag.String("hostname", "tailnetsdk-quickstart", "device name in the tailnet")
	dir := flag.String("dir", "", "state directory (defaults to the user config dir)")
	wait := flag.Bool("wait", true, "wait until the device is authorized")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if *wait {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 5*time.Minute)
		defer cancel()
	}

	cfg := core.Config{Dir: *dir, Hostname: *hostname}
	cfg.UserLogf = func(format string, args ...any) {
		fmt.Fprintf(os.Stderr, "[tsnet] "+format+"\n", args...)
	}

	node, err := core.New(cfg)
	if err != nil {
		fail(err)
	}
	defer node.Close()

	if err := node.Start(ctx); err != nil {
		fail(err)
	}

	if url, err := node.LoginURL(ctx); err == nil {
		fmt.Println("Open this URL in a browser to authorize this device:")
		fmt.Println("  " + url)
	} else if core.IsCode(err, core.ErrCodeBackend) {
		fmt.Println("device is already authorized")
	} else {
		fail(err)
	}

	if !*wait {
		return
	}
	st, err := node.WaitForRunning(ctx)
	if err != nil {
		fail(err)
	}
	printStatus(st)
}

func printStatus(st *core.Status) {
	fmt.Printf("\nstate:       %s\n", st.State)
	if st.Self != nil {
		fmt.Printf("device:      %s (%s)\n", st.Self.DNSName, st.Self.ID)
	}
	fmt.Printf("tailnet:     %s\n", st.TailnetName)
	fmt.Printf("tailnet IPs: %s\n", strings.Join(st.TailscaleIPs, ", "))
	fmt.Printf("peers:       %d (%d online)\n", len(st.Peers), st.OnlinePeerCount())
	for _, p := range st.Peers {
		ip := ""
		if len(p.IPs) > 0 {
			ip = p.IPs[0]
		}
		fmt.Printf("  - %-28s %-8s %-16s online=%v\n", p.DNSName, p.OS, ip, p.Online)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}
