package main

import "testing"

func TestGetenv(t *testing.T) {
	t.Setenv("DATABASE_KEY", "custom-status-key")

	if got := getenv("DATABASE_KEY", "nostr-status-lastfm"); got != "custom-status-key" {
		t.Fatalf("getenv returned %q, want %q", got, "custom-status-key")
	}
}

func TestGetenvFallback(t *testing.T) {
	if got := getenv("DATABASE_KEY", "nostr-status-lastfm"); got != "nostr-status-lastfm" {
		t.Fatalf("getenv returned %q, want %q", got, "nostr-status-lastfm")
	}
}
