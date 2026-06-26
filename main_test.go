package main

import (
	"testing"

	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip19"
)

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

func TestPublishEventFailsWithoutRelaySuccess(t *testing.T) {
	sk := nostr.GeneratePrivateKey()
	nsec, err := nip19.EncodePrivateKey(sk)
	if err != nil {
		t.Fatal(err)
	}

	oldPostRelays := postRelays
	postRelays = nil
	t.Cleanup(func() {
		postRelays = oldPostRelays
	})

	if err := publishEvent(nsec, "artist - track"); err == nil {
		t.Fatal("publishEvent returned nil, want error")
	}
}

func TestPublishEventReturnsInvalidNsecError(t *testing.T) {
	if err := publishEvent("not-a-nostr-private-key", "artist - track"); err == nil {
		t.Fatal("publishEvent returned nil, want error")
	}
}

func TestPublishEventRejectsNonPrivateNIP19Key(t *testing.T) {
	sk := nostr.GeneratePrivateKey()
	pub, err := nostr.GetPublicKey(sk)
	if err != nil {
		t.Fatal(err)
	}
	npub, err := nip19.EncodePublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}

	if err := publishEvent(npub, "artist - track"); err == nil {
		t.Fatal("publishEvent returned nil, want error")
	}
}
