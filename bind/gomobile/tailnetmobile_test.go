// Copyright (c) tailnetSDK contributors
// SPDX-License-Identifier: BSD-3-Clause

package tailnetmobile

import (
	"path/filepath"
	"testing"
)

func TestMobileNodeLifecycleOffline(t *testing.T) {
	n := NewNode()
	if n == nil {
		t.Fatal("expected non-nil node")
	}

	// Before configure, methods should return clean errors/empty strings
	if st := n.State(); st != "Unknown" {
		t.Fatalf("expected Unknown state, got %s", st)
	}
	if ip := n.TailnetIP(); ip != "" {
		t.Fatalf("expected empty IP, got %s", ip)
	}

	tmpDir := t.TempDir()
	err := n.Configure(filepath.Join(tmpDir, "mobile-state"), "mobile-test-node", "", "", true)
	if err != nil {
		t.Fatalf("Configure failed: %v", err)
	}

	if st := n.State(); st != "NoState" {
		t.Fatalf("expected NoState after configure, got %s", st)
	}

	// Close cleanly
	if err := n.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}
}
