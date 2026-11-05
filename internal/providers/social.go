package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/agim/lidza/pkg/router"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"golang.org/x/net/html"
	"zing/schema"
)

func Plain(s string) string {
	z := html.NewTokenizer(strings.NewReader(s))
	var b strings.Builder
	for {
		switch z.Next() {
		case html.ErrorToken:
			return strings.TrimSpace(b.String())
		case html.TextToken:
			b.Write(z.Text())
		case html.EndTagToken:
			name, _ := z.TagName()
			if string(name) == "p" || string(name) == "div" {
				b.WriteString("\n")
			}
		case html.StartTagToken:
			name, _ := z.TagName()
			if string(name) == "br" {
				b.WriteString("\n")
			}
		}
	}
}

type MAccount struct {
	ID          string `json:"id"`
	Acct        string `json:"acct"`
	DisplayName string `json:"display_name"`
	Avatar      string `json:"avatar"`
	Note        string `json:"note"`
	Followers   int    `json:"followers_count"`
	Following   int    `json:"following_count"`
}

func (a MAccount) View() schema.SocialAccount {
	return schema.SocialAccount{ID: a.ID, Handle: a.Acct, Name: a.DisplayName, Avatar: a.Avatar, Bio: Plain(a.Note), Followers: a.Followers, Following: a.Following}
}

type MPost struct {
	ID        string   `json:"id"`
	Content   string   `json:"content"`
	CreatedAt string   `json:"created_at"`
	URL       string   `json:"url"`
	Account   MAccount `json:"account"`
	Media     []struct {
		URL string `json:"url"`
	} `json:"media_attachments"`
	Likes      int    `json:"favourites_count"`
	Replies    int    `json:"replies_count"`
	Reposts    int    `json:"reblogs_count"`
	Liked      bool   `json:"favourited"`
	Reposted   bool   `json:"reblogged"`
	Visibility string `json:"visibility"`
	Reblog     *MPost `json:"reblog"`
}

func (p MPost) View() schema.SocialPost {
	if p.Reblog != nil {
		return p.Reblog.View()
	}
	out := schema.SocialPost{ID: p.ID, Text: Plain(p.Content), CreatedAt: p.CreatedAt, URL: p.URL, Author: p.Account.View(), Images: []string{}, Likes: p.Likes, Replies: p.Replies, Reposts: p.Reposts, Liked: p.Liked, Reposted: p.Reposted, Visibility: p.Visibility}
	for _, m := range p.Media {
		out.Images = append(out.Images, m.URL)
	}
	return out
}

type BAccount struct {
	DID         string `json:"did"`
	Handle      string `json:"handle"`
	DisplayName string `json:"displayName"`
	Avatar      string `json:"avatar"`
	Description string `json:"description"`
	Followers   int    `json:"followersCount"`
	Following   int    `json:"followsCount"`
}

func (a BAccount) View() schema.SocialAccount {
	return schema.SocialAccount{ID: a.DID, Handle: a.Handle, Name: a.DisplayName, Avatar: a.Avatar, Bio: a.Description, Followers: a.Followers, Following: a.Following}
}

type Ref struct {
	URI string `json:"uri"`
	CID string `json:"cid"`
}
type BPost struct {
	URI    string   `json:"uri"`
	CID    string   `json:"cid"`
	Author BAccount `json:"author"`
	Record struct {
		Text      string `json:"text"`
		CreatedAt string `json:"createdAt"`
		Reply     *struct {
			Root   Ref `json:"root"`
			Parent Ref `json:"parent"`
		} `json:"reply"`
	} `json:"record"`
	Embed struct {
		Images []struct {
			Fullsize string `json:"fullsize"`
		} `json:"images"`
	} `json:"embed"`
	Likes   int `json:"likeCount"`
	Replies int `json:"replyCount"`
	Reposts int `json:"repostCount"`
	Viewer  struct {
		Like   string `json:"like"`
		Repost string `json:"repost"`
	} `json:"viewer"`
}

func (p BPost) View() schema.SocialPost {
	parts := strings.Split(p.URI, "/")
	key := parts[len(parts)-1]
	out := schema.SocialPost{ID: p.URI, Cid: p.CID, Text: p.Record.Text, CreatedAt: p.Record.CreatedAt, URL: "https://bsky.app/profile/" + url.PathEscape(p.Author.Handle) + "/post/" + url.PathEscape(key), Author: p.Author.View(), Images: []string{}, Likes: p.Likes, Replies: p.Replies, Reposts: p.Reposts, Liked: p.Viewer.Like != "", Reposted: p.Viewer.Repost != "", Visibility: "public", RootID: p.URI, RootCid: p.CID}
	if p.Record.Reply != nil {
		out.RootID = p.Record.Reply.Root.URI
		out.RootCid = p.Record.Reply.Root.CID
	}
	for _, m := range p.Embed.Images {
		out.Images = append(out.Images, m.Fullsize)
	}
	return out
}
func (s Session) Feed(ctx context.Context, cursor, query string) (schema.FeedPage, error) {
	if s.LocalView && s.Network == "bluesky" {
		if s.View == nil {
			return schema.FeedPage{}, Unsupported()
		}
		return s.View.Feed(ctx, cursor, query)
	}
	out := schema.FeedPage{Items: []schema.SocialPost{}, Warnings: []string{}}
	switch s.Network {
	case "mastodon":
		var posts []MPost
		path := "/api/v1/timelines/home?limit=20&max_id=" + url.QueryEscape(cursor)
		if query != "" {
			var search struct {
				Statuses []MPost `json:"statuses"`
			}
			e := s.Do(ctx, "GET", "/api/v2/search?type=statuses&limit=20&q="+url.QueryEscape(query), nil, &search)
			if e != nil {
				return out, e
			}
			posts = search.Statuses
		} else if e := s.Do(ctx, "GET", path, nil, &posts); e != nil {
			return out, e
		}
		for _, p := range posts {
			out.Items = append(out.Items, p.View())
			out.Cursor = p.ID
		}
		return out, nil
	case "bluesky":
		var reply struct {
			Feed []struct {
				Post BPost `json:"post"`
			} `json:"feed"`
			Posts  []BPost `json:"posts"`
			Cursor string  `json:"cursor"`
		}
		path := "/xrpc/app.bsky.feed.getTimeline?limit=20&cursor=" + url.QueryEscape(cursor)
		if query != "" {
			path = "/xrpc/app.bsky.feed.searchPosts?limit=20&q=" + url.QueryEscape(query) + "&cursor=" + url.QueryEscape(cursor)
		}
		if e := s.Do(ctx, "GET", path, nil, &reply); e != nil {
			return out, e
		}
		for _, p := range reply.Feed {
			out.Items = append(out.Items, p.Post.View())
		}
		for _, p := range reply.Posts {
			out.Items = append(out.Items, p.View())
		}
		out.Cursor = reply.Cursor
		return out, nil
	case "threads":
		if query != "" {
			return out, Unsupported()
		}
		var reply struct {
			Data []struct {
				ID        string `json:"id"`
				Text      string `json:"text"`
				Timestamp string `json:"timestamp"`
				Permalink string `json:"permalink"`
				Username  string `json:"username"`
			} `json:"data"`
			Paging struct {
				Cursors struct {
					After string `json:"after"`
				} `json:"cursors"`
			} `json:"paging"`
		}
		if e := s.Do(ctx, "GET", "/v1.0/me/threads?fields=id,text,timestamp,permalink,username&limit=20&after="+url.QueryEscape(cursor), nil, &reply); e != nil {
			return out, e
		}
		for _, p := range reply.Data {
			out.Items = append(out.Items, schema.SocialPost{ID: p.ID, Text: p.Text, CreatedAt: p.Timestamp, URL: p.Permalink, Author: schema.SocialAccount{ID: s.Subject, Handle: p.Username, Name: p.Username}, Images: []string{}, Visibility: "public"})
		}
		out.Cursor = reply.Paging.Cursors.After
		out.Warnings = append(out.Warnings, "Threads exposes your own posts here, not a home timeline.")
		return out, nil
	}
	return out, Unsupported()
}
func (s Session) Profile(ctx context.Context, id string) (schema.SocialAccount, error) {
	if s.LocalView && s.Network == "bluesky" {
		if s.View == nil {
			return schema.SocialAccount{}, Unsupported()
		}
		return s.View.Profile(ctx, id)
	}
	switch s.Network {
	case "mastodon":
		var p MAccount
		e := s.Do(ctx, "GET", "/api/v1/accounts/"+url.PathEscape(id), nil, &p)
		return p.View(), e
	case "bluesky":
		var p BAccount
		e := s.Do(ctx, "GET", "/xrpc/app.bsky.actor.getProfile?actor="+url.QueryEscape(id), nil, &p)
		return p.View(), e
	case "threads":
		var p struct {
			ID       string `json:"id"`
			Username string `json:"username"`
			Name     string `json:"name"`
			Bio      string `json:"threads_biography"`
			Avatar   string `json:"threads_profile_picture_url"`
		}
		if id != s.Subject {
			return schema.SocialAccount{}, Unsupported()
		}
		e := s.Do(ctx, "GET", "/v1.0/me?fields=id,username,name,threads_biography,threads_profile_picture_url", nil, &p)
		return schema.SocialAccount{ID: p.ID, Handle: p.Username, Name: p.Name, Bio: p.Bio, Avatar: p.Avatar}, e
	}
	return schema.SocialAccount{}, Unsupported()
}
func (s Session) Action(ctx context.Context, in schema.ActionInput) error {
	if s.Network == "mastodon" {
		var path string
		switch in.Action {
		case "like":
			path = "/api/v1/statuses/" + url.PathEscape(in.Target) + "/favourite"
		case "unlike":
			path = "/api/v1/statuses/" + url.PathEscape(in.Target) + "/unfavourite"
		case "repost":
			path = "/api/v1/statuses/" + url.PathEscape(in.Target) + "/reblog"
		case "unrepost":
			path = "/api/v1/statuses/" + url.PathEscape(in.Target) + "/unreblog"
		case "follow", "unfollow", "mute", "unmute", "block", "unblock":
			path = "/api/v1/accounts/" + url.PathEscape(in.Target) + "/" + in.Action
		default:
			return Unsupported()
		}
		return s.Do(ctx, "POST", path, map[string]string{}, nil)
	}
	if s.Network != "bluesky" {
		return Unsupported()
	}
	if s.LocalView && (in.Action == "unlike" || in.Action == "unrepost" || in.Action == "unfollow" || in.Action == "unblock") {
		return s.hostedUndo(ctx, in)
	}
	if s.LocalView && (in.Action == "mute" || in.Action == "unmute") {
		return Unsupported()
	}
	if in.Action == "mute" || in.Action == "unmute" {
		return s.Do(ctx, "POST", "/xrpc/app.bsky.graph."+in.Action+"Actor", map[string]string{"actor": in.Target}, nil)
	}
	if in.Action == "unlike" || in.Action == "unrepost" || in.Action == "unfollow" || in.Action == "unblock" {
		var uri string
		if in.Action == "unlike" || in.Action == "unrepost" {
			var reply struct {
				Posts []BPost `json:"posts"`
			}
			if e := s.Do(ctx, "GET", "/xrpc/app.bsky.feed.getPosts?uris="+url.QueryEscape(in.Target), nil, &reply); e != nil {
				return e
			}
			if len(reply.Posts) != 1 {
				return Unsupported()
			}
			uri = reply.Posts[0].Viewer.Like
			if in.Action == "unrepost" {
				uri = reply.Posts[0].Viewer.Repost
			}
		} else {
			var p struct {
				Viewer struct {
					Following string `json:"following"`
					Blocking  string `json:"blocking"`
				} `json:"viewer"`
			}
			if e := s.Do(ctx, "GET", "/xrpc/app.bsky.actor.getProfile?actor="+url.QueryEscape(in.Target), nil, &p); e != nil {
				return e
			}
			uri = p.Viewer.Following
			if in.Action == "unblock" {
				uri = p.Viewer.Blocking
			}
		}
		if uri == "" {
			return nil
		}
		parts := strings.Split(strings.TrimPrefix(uri, "at://"), "/")
		if len(parts) != 3 || parts[0] != s.Subject {
			return Unsupported()
		}
		return s.Do(ctx, "POST", "/xrpc/com.atproto.repo.deleteRecord", map[string]string{"repo": s.Subject, "collection": parts[1], "rkey": parts[2]}, nil)
	}
	var collection string
	var subject any
	switch in.Action {
	case "like":
		collection = "app.bsky.feed.like"
		subject = Ref{URI: in.Target, CID: in.Cid}
	case "repost":
		collection = "app.bsky.feed.repost"
		subject = Ref{URI: in.Target, CID: in.Cid}
	case "follow":
		collection = "app.bsky.graph.follow"
		subject = in.Target
	case "block":
		collection = "app.bsky.graph.block"
		subject = in.Target
	default:
		return Unsupported()
	}
	var result any
	if s.Written != nil {
		result = s.Written
	}
	return s.Do(ctx, "POST", "/xrpc/com.atproto.repo.createRecord", map[string]any{"repo": s.Subject, "collection": collection, "record": map[string]any{"$type": collection, "subject": subject, "createdAt": time.Now().UTC().Format(time.RFC3339Nano)}}, result)
}

type Image struct {
	Data             []byte
	ContentType, Alt string
	SourceURL        string
}

func (s Session) upload(ctx context.Context, image Image) (json.RawMessage, error) {
	if s.Network == "bluesky" {
		req, e := http.NewRequestWithContext(ctx, "POST", s.Server+"/xrpc/com.atproto.repo.uploadBlob", bytes.NewReader(image.Data))
		if e != nil {
			return nil, e
		}
		req.Header.Set("Content-Type", image.ContentType)
		req.Header.Set("Authorization", "Bearer "+s.Access)
		resp, e := s.Request(req)
		if e != nil {
			return nil, e
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			return nil, ProviderError(resp.StatusCode)
		}
		var b struct {
			Blob json.RawMessage `json:"blob"`
		}
		e = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&b)
		return b.Blob, e
	}
	var b bytes.Buffer
	w := multipart.NewWriter(&b)
	f, e := w.CreateFormFile("file", "image")
	if e != nil {
		return nil, e
	}
	if _, e = f.Write(image.Data); e != nil {
		return nil, e
	}
	if e = w.WriteField("description", image.Alt); e != nil {
		return nil, e
	}
	if e = w.Close(); e != nil {
		return nil, e
	}
	req, e := http.NewRequestWithContext(ctx, "POST", s.Server+"/api/v1/media", &b)
	if e != nil {
		return nil, e
	}
	req.Header.Set("Authorization", "Bearer "+s.Access)
	req.Header.Set("Content-Type", w.FormDataContentType())
	resp, e := s.Request(req)
	if e != nil {
		return nil, e
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, ProviderError(resp.StatusCode)
	}
	var m struct {
		ID string `json:"id"`
	}
	e = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&m)
	id, e2 := json.Marshal(m.ID)
	if e != nil {
		return nil, e
	}
	return id, e2
}
func (s Session) Publish(ctx context.Context, text, key string, reply *schema.ComposeInput, images []Image) (Ref, string, error) {
	switch s.Network {
	case "mastodon":
		ids := []string{}
		for _, image := range images {
			m, e := s.upload(ctx, image)
			if e != nil {
				return Ref{}, "", e
			}
			var id string
			if e = json.Unmarshal(m, &id); e != nil {
				return Ref{}, "", e
			}
			ids = append(ids, id)
		}
		body := map[string]any{"status": text, "visibility": "public", "media_ids": ids}
		if reply != nil && reply.ReplyID != "" {
			body["in_reply_to_id"] = reply.ReplyID
		}
		data, e := json.Marshal(body)
		if e != nil {
			return Ref{}, "", e
		}
		req, e := http.NewRequestWithContext(ctx, "POST", s.Server+"/api/v1/statuses", bytes.NewReader(data))
		if e != nil {
			return Ref{}, "", e
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+s.Access)
		req.Header.Set("Idempotency-Key", key)
		resp, e := s.Request(req)
		if e != nil {
			return Ref{}, "", e
		}
		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return Ref{}, "", ProviderError(resp.StatusCode)
		}
		var p MPost
		e = json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&p)
		return Ref{URI: p.ID}, p.URL, e
	case "bluesky":
		tid, keyErr := syntax.ParseTID(key)
		if keyErr != nil {
			return Ref{}, "", router.Errorf(422, "A persisted AT Protocol record key is required")
		}
		record := map[string]any{"$type": "app.bsky.feed.post", "text": text, "createdAt": tid.Time().Format(time.RFC3339Nano)}
		if reply != nil && reply.ReplyID != "" {
			record["reply"] = map[string]Ref{"parent": {URI: reply.ReplyID, CID: reply.ReplyCid}, "root": {URI: reply.RootID, CID: reply.RootCid}}
		}
		if len(images) > 0 {
			entries := []map[string]any{}
			for _, im := range images {
				blob, e := s.upload(ctx, im)
				if e != nil {
					return Ref{}, "", e
				}
				entries = append(entries, map[string]any{"alt": im.Alt, "image": blob})
			}
			record["embed"] = map[string]any{"$type": "app.bsky.embed.images", "images": entries}
		}
		// putRecord uses a stable key: reconciliation and retry cannot create a second post.
		rkey := tid.String()
		var ref Ref
		e := s.Do(ctx, "POST", "/xrpc/com.atproto.repo.putRecord", map[string]any{"repo": s.Subject, "collection": "app.bsky.feed.post", "rkey": rkey, "record": record}, &ref)
		if s.LocalView {
			return ref, s.Server + "/xrpc/com.atproto.repo.getRecord?" + url.Values{"repo": {s.Subject}, "collection": {"app.bsky.feed.post"}, "rkey": {rkey}}.Encode(), e
		}
		return ref, "https://bsky.app/profile/" + url.PathEscape(s.Handle) + "/post/" + rkey, e
	case "threads":

		values := url.Values{"media_type": {"TEXT"}, "text": {text}}
		if len(images) > 0 {
			children := []string{}
			for _, image := range images {
				u, e := url.Parse(image.SourceURL)
				if e != nil || u.Scheme != "https" || u.Hostname() == "" {
					return Ref{}, "", router.Errorf(422, "Threads images require HTTPS object storage")
				}
				if len(images) == 1 {
					values.Set("media_type", "IMAGE")
					values.Set("image_url", image.SourceURL)
					values.Set("alt_text", image.Alt)
				} else {
					var child struct {
						ID string `json:"id"`
					}
					if e = s.Do(ctx, "POST", "/v1.0/"+url.PathEscape(s.Subject)+"/threads", url.Values{"media_type": {"IMAGE"}, "image_url": {image.SourceURL}, "alt_text": {image.Alt}, "is_carousel_item": {"true"}}, &child); e != nil {
						return Ref{}, "", e
					}
					children = append(children, child.ID)
				}
			}
			if len(images) > 1 {
				values.Set("media_type", "CAROUSEL")
				values.Set("children", strings.Join(children, ","))
			}
		}

		if reply != nil && reply.ReplyID != "" {
			values.Set("reply_to_id", reply.ReplyID)
		}
		var container struct {
			ID string `json:"id"`
		}
		if e := s.Do(ctx, "POST", "/v1.0/"+url.PathEscape(s.Subject)+"/threads", values, &container); e != nil {
			return Ref{}, "", e
		}
		var result struct {
			ID string `json:"id"`
		}
		e := s.Do(ctx, "POST", "/v1.0/"+url.PathEscape(s.Subject)+"/threads_publish", url.Values{"creation_id": {container.ID}}, &result)
		return Ref{URI: result.ID}, "", e
	}
	return Ref{}, "", Unsupported()
}

// Post fetches a single canonical object for delivery reconciliation.
func (s Session) Post(ctx context.Context, id string) (schema.SocialPost, error) {
	if s.LocalView && s.Network == "bluesky" {
		if s.View == nil {
			return schema.SocialPost{}, Unsupported()
		}
		return s.View.Post(ctx, id)
	}
	switch s.Network {
	case "mastodon":
		var p MPost
		e := s.Do(ctx, "GET", "/api/v1/statuses/"+url.PathEscape(id), nil, &p)
		return p.View(), e
	case "bluesky":
		var reply struct {
			Posts []BPost `json:"posts"`
		}
		if e := s.Do(ctx, "GET", "/xrpc/app.bsky.feed.getPosts?uris="+url.QueryEscape(id), nil, &reply); e != nil {
			return schema.SocialPost{}, e
		}
		if len(reply.Posts) != 1 {
			return schema.SocialPost{}, Unsupported()
		}
		return reply.Posts[0].View(), nil
	}
	return schema.SocialPost{}, Unsupported()
}

func (s Session) People(ctx context.Context, query string) ([]schema.SocialAccount, error) {
	if s.LocalView && s.Network == "bluesky" {
		if s.View == nil {
			return nil, Unsupported()
		}
		return s.View.People(ctx, query)
	}
	items := []schema.SocialAccount{}
	switch s.Network {
	case "mastodon":
		var result struct {
			Accounts []MAccount `json:"accounts"`
		}
		if e := s.Do(ctx, "GET", "/api/v2/search?type=accounts&limit=20&q="+url.QueryEscape(query), nil, &result); e != nil {
			return items, e
		}
		for _, a := range result.Accounts {
			items = append(items, a.View())
		}
	case "bluesky":
		var result struct {
			Actors []BAccount `json:"actors"`
		}
		if e := s.Do(ctx, "GET", "/xrpc/app.bsky.actor.searchActors?limit=20&q="+url.QueryEscape(query), nil, &result); e != nil {
			return items, e
		}
		for _, a := range result.Actors {
			items = append(items, a.View())
		}
	default:
		return items, Unsupported()
	}
	return items, nil
}
