package publishing

import (
	"context"

	"github.com/agim/lidza/packs/db"
	"github.com/agim/lidza/pkg/router"
	"net/url"
	"zing/internal/providers"
	"zing/schema"
)

// resolveReply maps an explicit relay only to the user's own confirmed public
// publications. External/private posts never acquire an invented counterpart.
func resolveReply(ctx context.Context, owner, connection string, s providers.Session, in schema.ComposeInput) (schema.ComposeInput, error) {
	if in.RelayParentDraftID == "" {
		return in, nil
	}
	var id, cid string
	e := db.From(ctx).QueryRow(ctx, `SELECT remote_id,remote_cid FROM delivery WHERE owner_id=$1 AND draft_id::text=$2 AND connection_id::text=$3 AND state='published'`, owner, in.RelayParentDraftID, connection).Scan(&id, &cid)
	if e != nil {
		return in, router.Errorf(422, "Every relay destination must have a confirmed published parent")
	}
	in.ReplyConnectionID = connection
	in.ReplyID = id
	in.ReplyCid = cid
	in.RootID = id
	in.RootCid = cid
	if s.Network == "bluesky" {
		var reply struct {
			Posts []struct {
				Record struct {
					Reply *struct {
						Root providers.Ref `json:"root"`
					} `json:"reply"`
				} `json:"record"`
			} `json:"posts"`
		}
		if e = s.Do(ctx, "GET", "/xrpc/app.bsky.feed.getPosts?uris="+url.QueryEscape(id), nil, &reply); e != nil {
			return in, e
		}
		if len(reply.Posts) != 1 {
			return in, router.NotFound("Relay parent")
		}
		if reply.Posts[0].Record.Reply != nil {
			in.RootID = reply.Posts[0].Record.Reply.Root.URI
			in.RootCid = reply.Posts[0].Record.Reply.Root.CID
		}
	}
	return in, nil
}
