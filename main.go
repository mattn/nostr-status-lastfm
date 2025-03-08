package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"

	"cloud.google.com/go/firestore"
	"github.com/ndyakov/go-lastfm"
	"google.golang.org/api/option"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const name = "nostr-status-lastfm"

const version = "0.0.1"

var revision = "HEAD"

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
	for _, track := range resp.RecentTracks {
		if track.NowPlaying == "" {
			continue
		}
		status := fmt.Sprintf("%s - %s\n", track.Artist.Name, track.Name)
		if status == lastStatus {
			continue
		}
		_, err = doc.Set(ctx, map[string]any{
			"status": status,
		})
		if err != nil {
			log.Fatalln(err)
		}
		break
	}
}
