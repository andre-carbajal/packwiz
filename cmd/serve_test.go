package cmd

import "testing"

func TestServeBindsLoopbackByDefault(t *testing.T) {
	flag := serveCmd.Flags().Lookup("host")
	if flag == nil {
		t.Fatal("serve --host flag is missing")
	}
	if flag.DefValue != "127.0.0.1" {
		t.Fatalf("serve --host default = %q, want 127.0.0.1", flag.DefValue)
	}
	if got := serveAddress(flag.DefValue, "8080"); got != "127.0.0.1:8080" {
		t.Fatalf("default serve address = %q, want 127.0.0.1:8080", got)
	}
}

func TestServeCanBindExplicitExternalHost(t *testing.T) {
	if got := serveAddress("0.0.0.0", "8080"); got != "0.0.0.0:8080" {
		t.Fatalf("explicit serve address = %q, want 0.0.0.0:8080", got)
	}
}
