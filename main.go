package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip19"
	"github.com/ndyakov/go-lastfm"
	"github.com/redis/go-redis/v9"
)

const name = "nostr-status-lastfm"

const version = "0.0.23"

var revision = "HEAD"

var postRelays = []string{
	"wss://relay-jp.nostr.wirednet.jp",
	"wss://yabu.me",
	"wss://relay.damus.io",
	"wss://nostr.compile-error.net",
	"wss://cagliostr.compile-error.net",
	"wss://ruby-nostr-relay.compile-error.net",
}

func publishEvent(nsec string, content string) error {
	var pub string
	var err error
	prefix, value, err := nip19.Decode(nsec)
	if err != nil {
		return err
	}
	if prefix != "nsec" {
		return fmt.Errorf("expected nsec private key, got %s", prefix)
	}
	sk, ok := value.(string)
	if !ok || sk == "" {
		return fmt.Errorf("invalid nsec private key")
	}
	if pub, err = nostr.GetPublicKey(sk); err != nil {
		return err
	}

	var ev nostr.Event
	ev.PubKey = pub
	ev.Content = content
	ev.CreatedAt = nostr.Now()
	ev.Tags = nostr.Tags{
		nostr.Tag{"d", "music"},
		nostr.Tag{"expiration", fmt.Sprint(time.Now().Add(5 * time.Minute).Unix())},
		nostr.Tag{"r", "spotify:search:" + url.QueryEscape(content)},
	}
	ev.Kind = nostr.KindUserStatuses
	if err := ev.Sign(sk); err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	var published atomic.Int32
	for _, r := range postRelays {
		wg.Add(1)
		go func(r string) {
			defer wg.Done()
			log.Println("publishing", r)
			relay, err := nostr.RelayConnect(ctx, r)
			if err != nil {
				log.Println(err)
				return
			}
			defer relay.Close()
			if err := relay.Publish(ctx, ev); err != nil {
				log.Println(err)
				return
			}
			published.Add(1)
		}(r)
	}
	wg.Wait()
	if published.Load() == 0 {
		return fmt.Errorf("failed to publish event to any relay")
	}
	return nil
}

func getenv(key, fallback string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return fallback
}

// isTemporaryLastfmError reports whether err is a Last.fm API error that is
// expected to succeed on a later run.
func isTemporaryLastfmError(err error) bool {
	var lerr *lastfm.LastfmErrorResponse
	if !errors.As(err, &lerr) {
		return false
	}
	switch lerr.Code {
	case 8, 11, 16, 29: // operation failed, service offline, temporarily unavailable, rate limit
		return true
	}
	return false
}

var listenBrainzAPIBase = "https://api.listenbrainz.org"

// temporaryError marks failures that are expected to succeed on a later run.
type temporaryError struct{ err error }

func (e *temporaryError) Error() string { return e.err.Error() }
func (e *temporaryError) Unwrap() error { return e.err }

func isTemporaryError(err error) bool {
	var terr *temporaryError
	return errors.As(err, &terr) || isTemporaryLastfmError(err)
}

func retry(f func() (string, error)) (string, error) {
	var status string
	var err error
	for i := range 3 {
		if i > 0 {
			time.Sleep(2 * time.Second)
		}
		status, err = f()
		if err == nil {
			return status, nil
		}
	}
	return "", err
}

// lastfmNowPlaying returns "artist - track" of the track now playing on
// Last.fm, or empty string when nothing is playing.
func lastfmNowPlaying(apiKey, apiSecret, user string) (string, error) {
	api := lastfm.New(apiKey, apiSecret)
	resp, err := api.User.GetRecentTracks(user, 0, 1, 0, 0)
	if err != nil {
		return "", fmt.Errorf("api.User.GetRecentTracks: %w", err)
	}
	for _, track := range resp.RecentTracks {
		if track.NowPlaying == "" {
			continue
		}
		return fmt.Sprintf("%s - %s", track.Artist.Name, track.Name), nil
	}
	return "", nil
}

type listenBrainzPlayingNow struct {
	Payload struct {
		Listens []struct {
			PlayingNow    bool `json:"playing_now"`
			TrackMetadata struct {
				ArtistName string `json:"artist_name"`
				TrackName  string `json:"track_name"`
			} `json:"track_metadata"`
		} `json:"listens"`
	} `json:"payload"`
}

// listenBrainzNowPlaying returns "artist - track" of the track now playing on
// ListenBrainz, or empty string when nothing is playing.
func listenBrainzNowPlaying(ctx context.Context, user, token string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	u := listenBrainzAPIBase + "/1/user/" + url.PathEscape(user) + "/playing-now"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", name+"/"+version)
	if token != "" {
		req.Header.Set("Authorization", "Token "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", &temporaryError{fmt.Errorf("listenbrainz: %w", err)}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		err := fmt.Errorf("listenbrainz: %s", resp.Status)
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			return "", &temporaryError{err}
		}
		return "", err
	}

	var body listenBrainzPlayingNow
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", fmt.Errorf("listenbrainz: %w", err)
	}
	for _, l := range body.Payload.Listens {
		md := l.TrackMetadata
		if !l.PlayingNow || md.TrackName == "" {
			continue
		}
		if md.ArtistName == "" {
			return md.TrackName, nil
		}
		return fmt.Sprintf("%s - %s", md.ArtistName, md.TrackName), nil
	}
	return "", nil
}

func main() {
	var lastFmApiKey string
	var lastFmApiSecret string
	var lastFmUser string
	var listenBrainzUser string
	var listenBrainzToken string
	var databaseURL string
	var databaseKey string
	var showVersion bool
	flag.StringVar(&lastFmApiKey, "lastfm-api-key", os.Getenv("LASTFM_API_KEY"), "LastFM API Key")
	flag.StringVar(&lastFmApiSecret, "lastfm-api-secret", os.Getenv("LASTFM_API_SECRET"), "LastFM API Secret")
	flag.StringVar(&lastFmUser, "lastfm-user", os.Getenv("LASTFM_USER"), "LastFM User")
	flag.StringVar(&listenBrainzUser, "listenbrainz-user", os.Getenv("LISTENBRAINZ_USER"), "ListenBrainz User (takes precedence over Last.fm)")
	flag.StringVar(&listenBrainzToken, "listenbrainz-token", os.Getenv("LISTENBRAINZ_TOKEN"), "ListenBrainz User Token (optional)")
	flag.StringVar(&databaseURL, "database-url", os.Getenv("DATABASE_URL"), "Redis Database URL")
	flag.StringVar(&databaseKey, "database-key", getenv("DATABASE_KEY", "nostr-status-lastfm"), "Redis Database Key")
	flag.BoolVar(&showVersion, "v", false, "show version")

	flag.Parse()

	if showVersion {
		fmt.Println(version)
		os.Exit(0)
	}

	log.Println("version", version)

	nsec := os.Getenv("BOT_NSEC")
	if nsec == "" {
		log.Fatal("BOT_NSEC is required")
	}

	ctx := context.Background()

	opt, err := redis.ParseURL(databaseURL)
	if err != nil {
		log.Fatal("redis.ParseURL:", err)
	}
	client := redis.NewClient(opt)

	var lastStatus string

	err = client.Get(ctx, databaseKey).Scan(&lastStatus)
	if err != nil && err != redis.Nil {
		log.Fatal("client.Get:", err)
	}

	var status string
	switch {
	case listenBrainzUser != "":
		status, err = retry(func() (string, error) {
			return listenBrainzNowPlaying(ctx, listenBrainzUser, listenBrainzToken)
		})
	case lastFmUser != "":
		status, err = retry(func() (string, error) {
			return lastfmNowPlaying(lastFmApiKey, lastFmApiSecret, lastFmUser)
		})
	default:
		log.Fatal("LISTENBRAINZ_USER or LASTFM_USER is required")
	}
	if err != nil {
		if isTemporaryError(err) {
			log.Println("temporary failure, skipping:", err)
			return
		}
		log.Fatal(err)
	}

	log.Printf("status: %q, lastStatus: %q", status, lastStatus)
	if status == "" || status == lastStatus {
		return
	}

	if err = publishEvent(nsec, status); err != nil {
		log.Fatal("publishEvent:", err)
	}

	log.Println("updating...")
	for range 3 {
		err = client.Set(ctx, databaseKey, status, 5*time.Minute).Err()
		if err == nil {
			break
		}
		time.Sleep(2 * time.Second)
	}
	if err != nil {
		log.Println(err)
		os.Exit(1)
	}
}
