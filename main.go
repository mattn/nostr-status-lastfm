package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/url"
	"os"
	"time"

	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip19"
	"github.com/ndyakov/go-lastfm"
	"github.com/redis/go-redis/v9"
)

const name = "nostr-status-lastfm"

const version = "0.0.11"

var revision = "HEAD"

var postRelays = []string{
	"wss://relay-jp.nostr.wirednet.jp",
	"wss://yabu.me",
	"wss://relay.damus.io",
	"wss://nostr.compile-error.net",
}

func publishEvent(nsec string, content string) error {
	var sk string
	var pub string
	var err error
	if _, s, err := nip19.Decode(nsec); err != nil {
		log.Fatal(err)
	} else {
		sk = s.(string)
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
		log.Fatal(err)
	}

	for _, r := range postRelays {
		log.Println("publishing", r)
		relay, err := nostr.RelayConnect(context.Background(), r)
		if err != nil {
			log.Println(err)
			continue
		}
		err = relay.Publish(context.Background(), ev)
		if err != nil {
			log.Println(err)
		}
		relay.Close()
	}
	return nil
}

func main() {
	var lastFmApiKey string
	var lastFmApiSecret string
	var lastFmUser string
	var databaseURL string
	var showVersion bool
	flag.StringVar(&lastFmApiKey, "lastfm-api-key", os.Getenv("LASTFM_API_KEY"), "LastFM API Key")
	flag.StringVar(&lastFmApiSecret, "lastfm-api-secret", os.Getenv("LASTFM_API_SECRET"), "LastFM API Secret")
	flag.StringVar(&lastFmUser, "lastfm-user", os.Getenv("LASTFM_USER"), "LastFM User")
	flag.StringVar(&databaseURL, "database-url", os.Getenv("DATABASE_URL"), "Redis Database URL")
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
		log.Fatal(err)
	}
	client := redis.NewClient(opt)

	err = client.FlushDB(ctx).Err()
	if err != nil {
		log.Fatal(err)
	}

	var lastStatus string

	client.Get(ctx, "status").Scan(&lastStatus)

	api := lastfm.New(lastFmApiKey, lastFmApiSecret)

	var resp *lastfm.RecentTracksResponse
	for range 3 {
		resp, err = api.User.GetRecentTracks(lastFmUser, 0, 1, 0, 0)
		if err == nil {
			break
		}
		time.Sleep(2 * time.Second)
	}
	if err != nil {
		log.Fatal(err)
	}

	var status string
	for _, track := range resp.RecentTracks {
		if track.NowPlaying == "" {
			continue
		}
		status = fmt.Sprintf("%s - %s", track.Artist.Name, track.Name)
		log.Println("status: " + status)
		break
	}

	if status == "" || status == lastStatus {
		return
	}

	log.Println("updating...")
	for range 3 {
		err = client.Set(ctx, "status", status, 0).Err()
		if err == nil {
			break
		}
		time.Sleep(2 * time.Second)
	}
	if err != nil {
		log.Println(err)
		os.Exit(1)
	}

	if err = publishEvent(nsec, status); err != nil {
		log.Fatal(err)
	}
}
