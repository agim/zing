package providers

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"zing/schema"
)

// PDS-only local reads use signed repository records. They are intentionally
// labeled as local and never masquerade as the global Bluesky AppView/chat.
type repoRecord struct {
	URI   string          `json:"uri"`
	CID   string          `json:"cid"`
	Value json.RawMessage `json:"value"`
}

func (s Session) records(ctx context.Context, did, collection, cursor string, limit int) ([]repoRecord, string, error) {
	var result struct {
		Records []repoRecord `json:"records"`
		Cursor  string       `json:"cursor"`
	}
	q := url.Values{"repo": {did}, "collection": {collection}, "limit": {"100"}, "cursor": {cursor}}
	if limit == 20 {
		q.Set("limit", "20")
	}
	e := s.Do(ctx, "GET", "/xrpc/com.atproto.repo.listRecords?"+q.Encode(), nil, &result)
	return result.Records, result.Cursor, e
}
func (s Session) ownRecords(ctx context.Context, collection string) ([]repoRecord, error) {
	out := []repoRecord{}
	cursor := ""
	for i := 0; i < 10; i++ {
		rows, next, e := s.records(ctx, s.Subject, collection, cursor, 100)
		if e != nil {
			return nil, e
		}
		out = append(out, rows...)
		if next == "" {
			return out, nil
		}
		cursor = next
	}
	return nil, Unsupported()
}
func (s Session) hostedUndo(ctx context.Context, in schema.ActionInput) error {
	collections := map[string]string{"unlike": "app.bsky.feed.like", "unrepost": "app.bsky.feed.repost", "unfollow": "app.bsky.graph.follow", "unblock": "app.bsky.graph.block"}
	collection := collections[in.Action]
	if collection == "" {
		return Unsupported()
	}
	records, e := s.ownRecords(ctx, collection)
	if e != nil {
		return e
	}
	for _, r := range records {
		var v struct {
			Subject json.RawMessage `json:"subject"`
		}
		if e = json.Unmarshal(r.Value, &v); e != nil {
			return e
		}
		var target string
		if collection == "app.bsky.graph.follow" || collection == "app.bsky.graph.block" {
			if e = json.Unmarshal(v.Subject, &target); e != nil {
				return e
			}
		} else {
			var ref Ref
			if e = json.Unmarshal(v.Subject, &ref); e != nil {
				return e
			}
			target = ref.URI
		}
		if target == in.Target {
			parts := strings.Split(r.URI, "/")
			if e = s.Do(ctx, "POST", "/xrpc/com.atproto.repo.deleteRecord", map[string]string{"repo": s.Subject, "collection": collection, "rkey": parts[len(parts)-1]}, nil); e != nil {
				return e
			}
		}
	}
	return nil
}
