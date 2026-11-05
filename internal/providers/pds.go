package providers

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/url"
	"sort"
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
func (s Session) hostedProfile(ctx context.Context, id string) (schema.SocialAccount, error) {
	for _, a := range s.Actors {
		if a.DID == id || a.Handle == id {
			out := schema.SocialAccount{ID: a.DID, Handle: a.Handle, Name: a.Handle}
			for _, actor := range s.Actors {
				reader := s
				reader.Subject = actor.DID
				records, e := reader.ownRecords(ctx, "app.bsky.graph.follow")
				if e != nil {
					return out, e
				}
				if actor.DID == a.DID {
					out.Following = len(records)
				}
				for _, r := range records {
					var v struct {
						Subject string `json:"subject"`
					}
					if e = json.Unmarshal(r.Value, &v); e != nil {
						return out, e
					}
					if v.Subject == a.DID {
						out.Followers++
					}
				}
			}
			return out, nil
		}
	}
	return schema.SocialAccount{}, Unsupported()
}
func (s Session) hostedPost(ctx context.Context, id string) (schema.SocialPost, error) {
	p := strings.Split(strings.TrimPrefix(id, "at://"), "/")
	if len(p) != 3 || p[1] != "app.bsky.feed.post" {
		return schema.SocialPost{}, Unsupported()
	}
	author, e := s.hostedProfile(ctx, p[0])
	if e != nil {
		return schema.SocialPost{}, e
	}
	var record repoRecord
	e = s.Do(ctx, "GET", "/xrpc/com.atproto.repo.getRecord?"+url.Values{"repo": {p[0]}, "collection": {p[1]}, "rkey": {p[2]}}.Encode(), nil, &record)
	if e != nil {
		return schema.SocialPost{}, e
	}
	var post BPost
	post.URI = record.URI
	post.CID = record.CID
	post.Author = BAccount{DID: author.ID, Handle: author.Handle, DisplayName: author.Name}
	if e = json.Unmarshal(record.Value, &post.Record); e != nil {
		return schema.SocialPost{}, e
	}
	v := post.View()
	addRepoImages(s.Server, p[0], record.Value, &v)
	v.URL = s.Server + "/xrpc/com.atproto.repo.getRecord?" + url.Values{"repo": {p[0]}, "collection": {p[1]}, "rkey": {p[2]}}.Encode()
	return v, nil
}
func (s Session) hostedFeed(ctx context.Context, cursor, query string) (schema.FeedPage, error) {
	out := schema.FeedPage{Items: []schema.SocialPost{}, Warnings: []string{"Local PDS repositories. Global Bluesky discovery, notifications and chat require separate services."}}
	cursors := map[string]string{}
	if cursor != "" {
		raw, e := base64.RawURLEncoding.DecodeString(cursor)
		if e != nil {
			return out, Unsupported()
		}
		if e = json.Unmarshal(raw, &cursors); e != nil {
			return out, Unsupported()
		}
	}
	blocked := map[string]bool{}
	blocks, e := s.ownRecords(ctx, "app.bsky.graph.block")
	if e != nil {
		return out, e
	}
	for _, r := range blocks {
		var v struct {
			Subject string `json:"subject"`
		}
		if e = json.Unmarshal(r.Value, &v); e != nil {
			return out, e
		}
		blocked[v.Subject] = true
	}
	next := map[string]string{}
	more := false
	for _, a := range s.Actors {
		if blocked[a.DID] || cursors[a.DID] == "~end" {
			next[a.DID] = "~end"
			continue
		}
		records, c, e := s.records(ctx, a.DID, "app.bsky.feed.post", cursors[a.DID], 20)
		if e != nil {
			return out, e
		}
		next[a.DID] = c
		if c == "" {
			next[a.DID] = "~end"
		} else {
			more = true
		}
		for _, r := range records {
			var post BPost
			post.URI = r.URI
			post.CID = r.CID
			post.Author = BAccount{DID: a.DID, Handle: a.Handle, DisplayName: a.Handle}
			if e = json.Unmarshal(r.Value, &post.Record); e != nil {
				return out, e
			}
			if query != "" && !strings.Contains(strings.ToLower(post.Record.Text), strings.ToLower(query)) {
				continue
			}
			v := post.View()
			addRepoImages(s.Server, a.DID, r.Value, &v)
			parts := strings.Split(r.URI, "/")
			v.URL = s.Server + "/xrpc/com.atproto.repo.getRecord?" + url.Values{"repo": {a.DID}, "collection": {"app.bsky.feed.post"}, "rkey": {parts[len(parts)-1]}}.Encode()
			out.Items = append(out.Items, v)
		}
	}
	// Read actual reaction records for displayed posts, including this viewer's
	// state. No invented counters or locally simulated social actions.
	byID := map[string]*schema.SocialPost{}
	for i := range out.Items {
		byID[out.Items[i].ID] = &out.Items[i]
	}
	for _, a := range s.Actors {
		for _, collection := range []string{"app.bsky.feed.like", "app.bsky.feed.repost"} {
			reader := s
			reader.Subject = a.DID
			records, e := reader.ownRecords(ctx, collection)
			if e != nil {
				return out, e
			}
			for _, r := range records {
				var v struct {
					Subject Ref `json:"subject"`
				}
				if e = json.Unmarshal(r.Value, &v); e != nil {
					return out, e
				}
				if p := byID[v.Subject.URI]; p != nil {
					if collection == "app.bsky.feed.like" {
						p.Likes++
						p.Liked = p.Liked || a.DID == s.Subject
					} else {
						p.Reposts++
						p.Reposted = p.Reposted || a.DID == s.Subject
					}
				}
			}
		}
	}
	sort.SliceStable(out.Items, func(i, j int) bool { return out.Items[i].CreatedAt > out.Items[j].CreatedAt })
	if more {
		raw, e := json.Marshal(next)
		if e != nil {
			return out, e
		}
		out.Cursor = base64.RawURLEncoding.EncodeToString(raw)
	}
	if len(s.Actors) >= 100 {
		out.Warnings = append(out.Warnings, "This local PDS view is limited to the first 100 hosted identities; configure a full AppView for a larger installation.")
	}
	return out, nil
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

func addRepoImages(origin, did string, raw json.RawMessage, out *schema.SocialPost) {
	var value struct {
		Embed struct {
			Images []struct {
				Image struct {
					Ref struct {
						Link string `json:"$link"`
					} `json:"ref"`
				} `json:"image"`
			} `json:"images"`
		} `json:"embed"`
	}
	if json.Unmarshal(raw, &value) != nil {
		return
	}
	for _, image := range value.Embed.Images {
		if image.Image.Ref.Link != "" {
			out.Images = append(out.Images, origin+"/xrpc/com.atproto.sync.getBlob?"+url.Values{"did": {did}, "cid": {image.Image.Ref.Link}}.Encode())
		}
	}
}
