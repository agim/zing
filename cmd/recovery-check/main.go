// recovery-check runs against an isolated restored database and master key.
// It emits counts only; credentials and private bodies never leave the process.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/agim/lidza/packs/db"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
	"zing/internal/platform/vault"
)

var pdsOrigin = flag.String("pds", "", "Optional isolated loopback PDS")

func check() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, e := db.Open(ctx, db.Config{URL: os.Getenv("DATABASE_URL")})
	if e != nil {
		return fmt.Errorf("isolated database unavailable")
	}
	defer pool.Close()
	checked := 0
	for _, source := range []string{"SELECT access_sealed FROM social_connection", "SELECT refresh_sealed FROM social_connection", "SELECT password_sealed FROM hosted_identity", "SELECT text_sealed FROM chat_message", "SELECT evidence_sealed FROM moderation_report", "SELECT password_sealed FROM backend_credential", "SELECT access_sealed FROM backend_credential"} {
		rows, e := pool.Query(ctx, source)
		if e != nil {
			return fmt.Errorf("snapshot schema check failed")
		}
		for rows.Next() {
			var sealed string
			if e = rows.Scan(&sealed); e != nil {
				rows.Close()
				return fmt.Errorf("snapshot value read failed")
			}
			if sealed == "" {
				continue
			}
			if _, e = vault.Open(sealed); e != nil {
				rows.Close()
				return fmt.Errorf("restored master key could not open private data")
			}
			checked++
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return fmt.Errorf("snapshot read failed")
		}
	}
	if checked == 0 {
		return fmt.Errorf("no private records were checked")
	}
	result := map[string]any{"status": "ok", "privateValuesDecrypted": checked}
	if *pdsOrigin != "" {
		u, e := url.Parse(*pdsOrigin)
		if e != nil || u.Scheme != "http" || (u.Hostname() != "127.0.0.1" && u.Hostname() != "localhost") {
			return fmt.Errorf("native recovery probe must use loopback")
		}
		var did, handle, sealed string
		if e = pool.QueryRow(ctx, `SELECT subject,handle,password_sealed FROM hosted_identity WHERE network='bluesky' AND state='active' LIMIT 1`).Scan(&did, &handle, &sealed); e != nil {
			return fmt.Errorf("no native PDS fixture in snapshot")
		}
		password, e := vault.Open(sealed)
		if e != nil {
			return fmt.Errorf("native credential could not be opened")
		}
		body, e := json.Marshal(map[string]string{"identifier": handle, "password": password})
		if e != nil {
			return e
		}
		req, e := http.NewRequestWithContext(ctx, "POST", *pdsOrigin+"/xrpc/com.atproto.server.createSession", bytes.NewReader(body))
		if e != nil {
			return e
		}
		req.Header.Set("Content-Type", "application/json")
		client := &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		response, e := client.Do(req)
		if e != nil {
			return fmt.Errorf("native PDS login unavailable")
		}
		var session struct {
			DID string `json:"did"`
		}
		e = json.NewDecoder(response.Body).Decode(&session)
		closeErr := response.Body.Close()
		if e != nil || closeErr != nil || response.StatusCode != 200 || session.DID != did {
			return fmt.Errorf("native PDS identity was not preserved")
		}
		response, e = client.Get(*pdsOrigin + "/xrpc/com.atproto.repo.listRecords?" + url.Values{"repo": {did}, "collection": {"app.bsky.feed.post"}, "limit": {"1"}}.Encode())
		if e != nil {
			return fmt.Errorf("native repository unavailable")
		}
		var records struct{ Records []struct{ URI, CID string } }
		e = json.NewDecoder(response.Body).Decode(&records)
		closeErr = response.Body.Close()
		if e != nil || closeErr != nil || response.StatusCode != 200 || len(records.Records) == 0 || !strings.HasPrefix(records.Records[0].URI, "at://"+did+"/") || records.Records[0].CID == "" {
			return fmt.Errorf("native repository record missing")
		}
		result["nativeIdentityAndRepositoryPreserved"] = true
	}
	return json.NewEncoder(os.Stdout).Encode(result)
}
func main() {
	flag.Parse()
	if e := check(); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
