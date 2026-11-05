package atview

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"github.com/agim/lidza/packs/db"
	"github.com/agim/lidza/pkg/router"
	"github.com/jackc/pgx/v5"
	"net/url"
	"strings"
	"time"
	queries "zing/db/queries/gen"
	"zing/internal/providers"
	"zing/schema"
)

type Reader struct{ Owner, DID, Server, Scope string }
type cursor struct {
	At                     time.Time `json:"at"`
	URI, DID, Scope, Query string
}

func (r Reader) Feed(ctx context.Context, token, search string) (schema.FeedPage, error) {
	out := schema.FeedPage{Items: []schema.SocialPost{}, Warnings: []string{}}
	mode := r.Scope
	if mode == "" {
		mode = "following"
	}
	if mode != "following" && mode != "discover" {
		return out, router.Errorf(422, "Choose Following or Discover")
	}
	input := queries.AtFeedParams{ViewerDid: r.DID, OwnerID: r.Owner, Scope: mode, Query: search}
	if token != "" {
		var c cursor
		if len(token) > 4096 {
			return out, router.Errorf(422, "Invalid feed cursor")
		}
		raw, e := base64.RawURLEncoding.DecodeString(token)
		if e != nil {
			return out, router.Errorf(422, "Invalid feed cursor")
		}
		if e = json.Unmarshal(raw, &c); e != nil || c.DID != r.DID || c.Scope != mode || c.Query != search {
			return out, router.Errorf(422, "This cursor belongs to another feed")
		}
		input.CursorAt = &c.At
		input.CursorUri = c.URI
	}
	rows, e := queries.New(db.From(ctx)).AtFeed(ctx, input)
	if e != nil {
		return out, e
	}
	if len(rows) > 20 {
		last := rows[19]
		encoded, err := json.Marshal(cursor{At: last.CreatedAt, URI: last.Uri, DID: r.DID, Scope: mode, Query: search})
		if err != nil {
			return out, err
		}
		out.Cursor = base64.RawURLEncoding.EncodeToString(encoded)
		rows = rows[:20]
	}
	for _, row := range rows {
		v, e := r.post(row)
		if e != nil {
			return out, e
		}
		out.Items = append(out.Items, v)
	}
	return out, nil
}
func (r Reader) post(row queries.AtFeedRow) (schema.SocialPost, error) {
	var source providers.BPost
	source.URI = row.Uri
	source.CID = row.Cid
	source.Author = providers.BAccount{DID: row.Did, Handle: row.Handle, DisplayName: row.AuthorName, Description: row.AuthorBio}
	if e := json.Unmarshal(row.Value, &source.Record); e != nil {
		return schema.SocialPost{}, e
	}
	out := source.View()
	out.Likes = int(row.Likes)
	out.Reposts = int(row.Reposts)
	out.Replies = int(row.Replies)
	out.Liked = row.Liked
	out.Reposted = row.Reposted
	out.Author.Avatar = blobImage(r.Server, row.Did, row.ProfileValue, "avatar")
	out.Images = recordImages(r.Server, row.Did, row.Value)
	parts := strings.Split(row.Uri, "/")
	out.URL = r.Server + "/xrpc/com.atproto.repo.getRecord?" + url.Values{"repo": {row.Did}, "collection": {"app.bsky.feed.post"}, "rkey": {parts[len(parts)-1]}}.Encode()
	return out, nil
}
func (r Reader) Post(ctx context.Context, id string) (schema.SocialPost, error) {
	rows, e := queries.New(db.From(ctx)).AtFeed(ctx, queries.AtFeedParams{ViewerDid: r.DID, OwnerID: r.Owner, Scope: "discover", PostUri: id})
	if e != nil {
		return schema.SocialPost{}, e
	}
	if len(rows) != 1 {
		return schema.SocialPost{}, router.NotFound("Post")
	}
	return r.post(rows[0])
}
func (r Reader) Profile(ctx context.Context, id string) (schema.SocialAccount, error) {
	q := queries.New(db.From(ctx))
	identity, e := q.AtIdentity(ctx, id)
	if errors.Is(e, pgx.ErrNoRows) {
		return schema.SocialAccount{}, router.NotFound("Hosted profile")
	}
	if e != nil {
		return schema.SocialAccount{}, e
	}
	row, e := q.AtProfile(ctx, queries.AtProfileParams{Subject: identity.Subject, Did: r.DID})
	if errors.Is(e, pgx.ErrNoRows) {
		return schema.SocialAccount{}, router.NotFound("Profile")
	}
	if e != nil {
		return schema.SocialAccount{}, e
	}
	return schema.SocialAccount{ID: row.Subject, Handle: row.Handle, Name: row.Name, Bio: row.Bio, Avatar: blobImage(row.Server, row.Subject, row.ProfileValue, "avatar"), Followers: int(row.Followers), Following: int(row.Following)}, nil
}
func (r Reader) People(ctx context.Context, search string) ([]schema.SocialAccount, error) {
	out := []schema.SocialAccount{}
	rows, e := queries.New(db.From(ctx)).AtPeople(ctx, queries.AtPeopleParams{Column1: search, Did: r.DID})
	if e != nil {
		return out, e
	}
	for _, row := range rows {
		out = append(out, schema.SocialAccount{ID: row.Subject, Handle: row.Handle, Name: row.Name, Bio: row.Bio, Avatar: blobImage(row.Server, row.Subject, row.ProfileValue, "avatar"), Followers: int(row.Followers), Following: int(row.Following)})
	}
	return out, nil
}
func (r Reader) Notifications(ctx context.Context) ([]schema.NotificationView, error) {
	out := []schema.NotificationView{}
	rows, e := queries.New(db.From(ctx)).AtNotifications(ctx, queries.AtNotificationsParams{Recipient: r.DID, OwnerID: r.Owner})
	if e != nil {
		return out, e
	}
	for _, row := range rows {
		text := row.Handle + " · " + row.Kind
		if row.Text != "" {
			text += " · " + row.Text
		}
		out = append(out, schema.NotificationView{ID: row.ID, Kind: row.Kind, Text: text, CreatedAt: row.CreatedAt.UTC().Format(time.RFC3339Nano)})
	}
	return out, nil
}
func blobImage(origin, did string, raw []byte, field string) string {
	var values map[string]json.RawMessage
	if json.Unmarshal(raw, &values) != nil {
		return ""
	}
	return blob(origin, did, values[field])
}
func blob(origin, did string, raw []byte) string {
	var value struct {
		Ref struct {
			Link string `json:"$link"`
		} `json:"ref"`
	}
	if json.Unmarshal(raw, &value) != nil || value.Ref.Link == "" {
		return ""
	}
	return origin + "/xrpc/com.atproto.sync.getBlob?" + url.Values{"did": {did}, "cid": {value.Ref.Link}}.Encode()
}
func recordImages(origin, did string, raw []byte) []string {
	out := []string{}
	var value struct {
		Embed struct {
			Images []struct {
				Image json.RawMessage `json:"image"`
			} `json:"images"`
		} `json:"embed"`
	}
	if json.Unmarshal(raw, &value) != nil {
		return out
	}
	for _, image := range value.Embed.Images {
		if path := blob(origin, did, image.Image); path != "" {
			out = append(out, path)
		}
	}
	return out
}
