package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip19"
	lastfm "github.com/ndyakov/go-lastfm"
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

func TestIsTemporaryLastfmError(t *testing.T) {
	if !isTemporaryLastfmError(&lastfm.LastfmErrorResponse{Code: 8, Message: "Operation failed"}) {
		t.Fatal("code 8 should be temporary")
	}
	if isTemporaryLastfmError(&lastfm.LastfmErrorResponse{Code: 6, Message: "User not found"}) {
		t.Fatal("code 6 should not be temporary")
	}
	if isTemporaryLastfmError(errors.New("other")) {
		t.Fatal("non-lastfm error should not be temporary")
	}
}

func withListenBrainzServer(t *testing.T, h http.HandlerFunc) {
	t.Helper()
	ts := httptest.NewServer(h)
	t.Cleanup(ts.Close)
	old := listenBrainzAPIBase
	listenBrainzAPIBase = ts.URL
	t.Cleanup(func() { listenBrainzAPIBase = old })
}

func TestListenBrainzNowPlaying(t *testing.T) {
	withListenBrainzServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/1/user/someone/playing-now" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Token tok" {
			t.Errorf("Authorization = %q", got)
		}
		w.Write([]byte(`{"payload":{"count":1,"listens":[{"playing_now":true,"track_metadata":{"artist_name":"Artist","track_name":"Track","release_name":"Album"}}],"playing_now":true,"user_id":"someone"}}`))
	})
	got, err := listenBrainzNowPlaying(context.Background(), "someone", "tok")
	if err != nil {
		t.Fatal(err)
	}
	if got != "Artist - Track" {
		t.Fatalf("got %q", got)
	}
}

func TestListenBrainzNothingPlaying(t *testing.T) {
	withListenBrainzServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"payload":{"count":0,"listens":[],"playing_now":true,"user_id":"someone"}}`))
	})
	got, err := listenBrainzNowPlaying(context.Background(), "someone", "")
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Fatalf("got %q, want empty", got)
	}
}

func TestListenBrainzErrors(t *testing.T) {
	for _, tc := range []struct {
		code      int
		temporary bool
	}{
		{http.StatusTooManyRequests, true},
		{http.StatusServiceUnavailable, true},
		{http.StatusNotFound, false},
	} {
		withListenBrainzServer(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(tc.code)
		})
		_, err := listenBrainzNowPlaying(context.Background(), "someone", "")
		if err == nil {
			t.Fatalf("%d: want error", tc.code)
		}
		if got := isTemporaryError(err); got != tc.temporary {
			t.Fatalf("%d: isTemporaryError = %v, want %v", tc.code, got, tc.temporary)
		}
	}
}
