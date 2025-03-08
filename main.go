package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/url"
	"os"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip19"
	"github.com/ndyakov/go-lastfm"
	"google.golang.org/api/option"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const name = "nostr-status-lastfm"

const version = "0.0.2"

var revision = "HEAD"

var postRelays = []string{
	"wss://relay-jp.nostr.wirednet.jp",
	"wss://yabu.me",
	"wss://relay.damus.io",
	"wss://nostr.compile-error.net",
}

func main() {
	var lastFmApiKey string
	var lastFmApiSecret string
	var lastFmUser string
	var firestoreJsonFile string
	var firestoreProjectID string
	var showVersion bool
	flag.StringVar(&lastFmApiKey, "lastfm-api-key", os.Getenv("LASTFM_API_KEY"), "LastFM API Key")
	flag.StringVar(&lastFmApiSecret, "lastfm-api-secret", os.Getenv("LASTFM_API_SECRET"), "LastFM API Secret")
	flag.StringVar(&lastFmUser, "lastfm-user", os.Getenv("LASTFM_USER"), "LastFM User")
	flag.StringVar(&firestoreJsonFile, "firestore-json-file", os.Getenv("FIRESTORE_JSON_FILE"), "Firestore JSON file")
	flag.StringVar(&firestoreProjectID, "firestore-project-id", os.Getenv("FIRESTORE_PROJECT_ID"), "Firestore Project ID")
	flag.BoolVar(&showVersion, "v", false, "show version")

	flag.Parse()

	if showVersion {
		fmt.Println(version)
		os.Exit(0)
	}

	nsec := os.Getenv("BOT_NSEC")
	if nsec == "" {
		log.Fatal("BOT_NSEC is required")
	}

	ctx := context.Background()
	sa := option.WithCredentialsFile(firestoreJsonFile)
	client, err := firestore.NewClient(ctx, firestoreProjectID, sa)
	if err != nil {
		log.Fatalln(err)
	}
	defer client.Close()

	doc := client.Collection(firestore.DefaultDatabaseID).Doc("nostr-status-lastfm")
	r, err := doc.Get(ctx)
	if err != nil {
		if status.Code(err) != codes.NotFound {
			log.Fatalln(err)
		}
	}
	var lastStatus string
	if v, ok := r.Data()["status"]; ok {
		lastStatus = v.(string)
	}

	api := lastfm.New(lastFmApiKey, lastFmApiSecret)

	resp, err := api.User.GetRecentTracks(lastFmUser, 0, 1, 0, 0)
	if err != nil {
		log.Fatal(err)
	}

	var status string
	for _, track := range resp.RecentTracks {
		if track.NowPlaying == "" {
			continue
		}
		curr := fmt.Sprintf("%s - %s\n", track.Artist.Name, track.Name)
		if curr == lastStatus {
			continue
		}
		status = curr
		log.Println(status)
		break
	}

	if status == "" || status == lastStatus {
		return
	}

	_, err = doc.Set(ctx, map[string]any{
		"status": status,
	})
	if err != nil {
		log.Fatalln(err)
	}

	var sk string
	var pub string
	if _, s, err := nip19.Decode(nsec); err != nil {
		log.Fatal(err)
	} else {
		sk = s.(string)
	}
	if pub, err = nostr.GetPublicKey(sk); err != nil {
		log.Fatal(err)
	}

	var ev nostr.Event
	ev.PubKey = pub
	ev.Content = status
	ev.CreatedAt = nostr.Now()
	ev.Tags = nostr.Tags{
		nostr.Tag{"d", "music"},
		nostr.Tag{"expiration", fmt.Sprint(time.Now().Add(5 * time.Minute).Unix())},
		nostr.Tag{"r", "spotify:search:" + url.QueryEscape(status)},
	}
	ev.Kind = nostr.KindUserStatuses
	if err := ev.Sign(sk); err != nil {
		log.Fatal(err)
	}
	for _, r := range postRelays {
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
}
