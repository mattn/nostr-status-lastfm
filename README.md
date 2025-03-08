# nostr-status-lastfm

Update Nostr kind 30315 status via Last.fm scrobble

## Usage

```
apiVersion: batch/v1
kind: CronJob
metadata:
  name: nostr-status-lastfm
spec:
  schedule: '* * * * *'
  successfulJobsHistoryLimit: 0
  failedJobsHistoryLimit: 1
  suspend: false
  concurrencyPolicy: Forbid
  startingDeadlineSeconds: 300
  jobTemplate:
    spec:
      parallelism: 1
      completions: 1
      backoffLimit: 1
      activeDeadlineSeconds: 1800
      template:
        spec:
          restartPolicy: Never
          containers:
          - name: nostr-status-lastfm
            image: ghcr.io/mattn/nostr-status-lastfm
            imagePullPolicy: IfNotPresent
            #imagePullPolicy: Always
            env:
            - name: BOT_NSEC
              value: 'nsec1...'
            - name: LASTFM_USER
              value: '...'
            - name: LASTFM_API_KEY
              value: '...'
            - name: LASTFM_API_SECRET
              value: '...'
            - name: DATABASE_URL
              value: 'rediss://...'
```

## Requirements

* Nostr account
* Last.fm account
* Redis database

## License

MIT

## Author

Yasuhiro Matsumoto (a.k.a. mattn)
