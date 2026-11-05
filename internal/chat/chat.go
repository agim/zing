// Package chat implements private Zing chat for locally hosted identities.
// Message bodies never enter public repositories or another network.
package chat

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/agim/lidza"
	"github.com/agim/lidza/packs/db"
	"github.com/agim/lidza/packs/jobs"
	"github.com/agim/lidza/packs/realtime"
	"github.com/agim/lidza/pkg/router"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"sort"
	"strings"
	"time"
	queries "zing/db/queries/gen"
	"zing/internal/platform/vault"
	"zing/schema"
)

type membership struct {
	State, Initiator, OtherDID, OtherOwner, OtherHandle string
	Count                                               int64
}

func allowed(ctx context.Context, source queries.DBTX, owner, did, otherDID, otherOwner string) error {
	blocked, e := queries.New(source).ChatAllowed(ctx, queries.ChatAllowedParams{OwnerID: owner, OwnerID_2: otherOwner, Did: did, Subject: otherDID})
	if e != nil {
		return e
	}
	if blocked == nil || *blocked {
		return router.Errorf(403, "This private conversation is unavailable because an account is blocked or suspended")
	}
	return nil
}
func member(ctx context.Context, source queries.DBTX, owner, did, id string) (membership, error) {
	var m membership
	if _, e := uuid.Parse(id); e != nil {
		return m, router.NotFound("Conversation")
	}
	row, e := queries.New(source).ChatMembership(ctx, queries.ChatMembershipParams{OwnerID: owner, Did: did, ID: id})
	if errors.Is(e, pgx.ErrNoRows) {
		return m, router.NotFound("Conversation")
	}
	if e != nil {
		return m, e
	}
	m = membership{State: row.State, Initiator: row.InitiatedBy, OtherDID: row.OtherDid, OtherOwner: row.OtherOwner, OtherHandle: row.OtherHandle, Count: row.MessageCount}
	if m.OtherOwner == "" {
		return m, nil
	}
	return m, allowed(ctx, source, owner, did, m.OtherDID, m.OtherOwner)
}
func view(id string, m membership, owner string) schema.ConversationView {
	return schema.ConversationView{ID: id, Service: "zing", Title: m.OtherHandle, Recipient: m.OtherDID, Pending: m.State == "pending", CanAccept: m.State == "pending" && m.Initiator != owner && m.OtherOwner != "", CanReply: m.OtherOwner != "" && (m.State == "accepted" || (m.Initiator == owner && m.Count == 0))}
}
func Open(ctx context.Context, owner, did, recipient string) (schema.ConversationView, error) {
	var out schema.ConversationView
	if recipient == did {
		return out, router.Errorf(422, "Choose another hosted identity")
	}
	otherOwner, e := queries.New(db.From(ctx)).ChatRecipient(ctx, recipient)
	if errors.Is(e, pgx.ErrNoRows) {
		return out, router.Errorf(422, "Zing chat is available only for identities hosted here. Connected external accounts use native Bluesky DMs.")
	}
	if e != nil {
		return out, e
	}
	if e = allowed(ctx, db.From(ctx), owner, did, recipient, otherOwner); e != nil {
		return out, e
	}
	pair := []string{did, recipient}
	sort.Strings(pair)
	sum := sha256.Sum256([]byte(strings.Join(pair, "\x00")))
	key := hex.EncodeToString(sum[:])
	tx, e := db.From(ctx).Begin(ctx)
	if e != nil {
		return out, e
	}
	defer tx.Rollback(ctx)
	q := queries.New(tx)
	owners := []string{owner, otherOwner}
	sort.Strings(owners)
	for _, o := range owners {
		if e = q.ChatLock(ctx, "chat-owner:"+o); e != nil {
			return out, e
		}
	}
	exists, err := q.ChatOwnersExist(ctx, queries.ChatOwnersExistParams{Subject: owner, Subject_2: otherOwner})
	if err != nil {
		return out, err
	}
	if !exists {
		return out, router.NotFound("User")
	}
	id, e := q.ChatCreate(ctx, queries.ChatCreateParams{PairKey: key, InitiatedBy: owner})
	if e != nil {
		return out, e
	}
	for _, m := range []struct{ owner, did string }{{owner, did}, {otherOwner, recipient}} {
		if e = q.ChatAddMember(ctx, queries.ChatAddMemberParams{ConversationID: id, OwnerID: m.owner, Did: m.did}); e != nil {
			return out, e
		}
	}
	m, e := member(ctx, tx, owner, did, id)
	if e != nil {
		return out, e
	}
	out = view(id, m, owner)
	return out, tx.Commit(ctx)
}
func List(ctx context.Context, owner, did string) ([]schema.ConversationView, error) {
	out := []schema.ConversationView{}
	q := queries.New(db.From(ctx))
	ids, e := q.ChatListIDs(ctx, queries.ChatListIDsParams{OwnerID: owner, Did: did})
	if e != nil {
		return out, e
	}
	for _, i := range ids {
		m, err := member(ctx, db.From(ctx), owner, did, i.ID)
		if err != nil {
			var h *router.HTTPError
			if errors.As(err, &h) && h.Status == 403 {
				continue
			}
			return out, err
		}
		c := view(i.ID, m, owner)
		c.Unread = i.LastReadAt == nil || i.UpdatedAt.After(*i.LastReadAt)
		sealed, err := q.ChatLastMessage(ctx, i.ID)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return out, err
		}
		if sealed != "" {
			c.LastText, e = vault.Open(sealed)
			if e != nil {
				return out, e
			}
		}
		out = append(out, c)
	}
	return out, nil
}
func Messages(ctx context.Context, owner, did, id, cursor string) (schema.MessageList, error) {
	out := schema.MessageList{Items: []schema.MessageView{}, Service: "zing"}
	q := queries.New(db.From(ctx))
	m, e := member(ctx, db.From(ctx), owner, did, id)
	if e != nil {
		return out, e
	}
	v := view(id, m, owner)
	out.CanReply = v.CanReply
	out.CanAccept = v.CanAccept
	if cursor != "" {
		if _, e = uuid.Parse(cursor); e != nil {
			return out, router.Errorf(422, "Invalid message cursor")
		}
		exists, err := q.ChatCursorExists(ctx, queries.ChatCursorExistsParams{ConversationID: id, ID: cursor})
		if err != nil {
			return out, err
		}
		if !exists {
			return out, router.Errorf(422, "Invalid message cursor")
		}
	}
	rows, e := q.ChatMessages(ctx, queries.ChatMessagesParams{ConversationID: id, Column2: cursor})
	if e != nil {
		return out, e
	}
	for _, r := range rows {
		text, err := vault.Open(r.TextSealed)
		if err != nil {
			return out, err
		}
		out.Items = append(out.Items, schema.MessageView{ID: r.ID, Sender: r.Sender, SenderDid: r.SenderDid, Text: text, CreatedAt: r.CreatedAt.UTC().Format(time.RFC3339Nano)})
	}
	if len(out.Items) > 50 {
		out.Items = out.Items[:50]
		out.Cursor = out.Items[len(out.Items)-1].ID
	}
	for l, r := 0, len(out.Items)-1; l < r; l, r = l+1, r-1 {
		out.Items[l], out.Items[r] = out.Items[r], out.Items[l]
	}
	if cursor == "" {
		if e = q.ChatMarkRead(ctx, queries.ChatMarkReadParams{OwnerID: owner, Did: did, ConversationID: id}); e != nil {
			return out, e
		}
	}
	return out, nil
}
func Send(ctx context.Context, owner, did string, in schema.MessageInput) error {
	if _, e := uuid.Parse(in.ClientID); e != nil {
		return router.Errorf(422, "Local messages need a client-generated UUID to prevent duplicate sends")
	}
	if strings.TrimSpace(in.Text) == "" {
		return router.Errorf(422, "Write a message first")
	}
	tx, e := db.From(ctx).Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	q := queries.New(tx)
	if e = q.ChatLock(ctx, "chat:"+in.ConversationID); e != nil {
		return e
	}
	m, e := member(ctx, tx, owner, did, in.ConversationID)
	if e != nil {
		return e
	}
	prior, e := q.ChatPriorMessage(ctx, queries.ChatPriorMessageParams{ConversationID: in.ConversationID, SenderID: owner, ClientID: in.ClientID})
	if e == nil {
		plain, err := vault.Open(prior)
		if err != nil {
			return err
		}
		if plain != in.Text {
			return router.Errorf(409, "This message identifier has already been used")
		}
		return nil
	}
	if !errors.Is(e, pgx.ErrNoRows) {
		return e
	}
	if !view(in.ConversationID, m, owner).CanReply {
		return router.Errorf(409, "The recipient must accept before another message can be sent")
	}
	sealed, e := vault.Seal(in.Text)
	if e != nil {
		return e
	}
	if e = q.ChatInsertMessage(ctx, queries.ChatInsertMessageParams{ConversationID: in.ConversationID, SenderID: owner, SenderDid: did, ClientID: in.ClientID, TextSealed: sealed}); e != nil {
		return e
	}
	if e = q.ChatTouch(ctx, in.ConversationID); e != nil {
		return e
	}
	if e = q.ChatSenderRead(ctx, queries.ChatSenderReadParams{ConversationID: in.ConversationID, OwnerID: owner}); e != nil {
		return e
	}
	if _, e = jobs.From(ctx).EnqueueTx(ctx, tx, "zing.chat-notify", in.ConversationID); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
func Accept(ctx context.Context, owner, did, id string) error {
	tx, e := db.From(ctx).Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	q := queries.New(tx)
	if e = q.ChatLock(ctx, "chat:"+id); e != nil {
		return e
	}
	m, e := member(ctx, tx, owner, did, id)
	if e != nil {
		return e
	}
	if !view(id, m, owner).CanAccept {
		return router.Errorf(409, "This conversation has no invitation for you to accept")
	}
	if e = q.ChatAccept(ctx, id); e != nil {
		return e
	}
	if _, e = jobs.From(ctx).EnqueueTx(ctx, tx, "zing.chat-notify", id); e != nil {
		return e
	}
	return tx.Commit(ctx)
}

type ReportEvidence struct{ Text, Sender, Owner string }

func Evidence(ctx context.Context, owner, did, id string) (ReportEvidence, error) {
	var out ReportEvidence
	r, e := queries.New(db.From(ctx)).ChatEvidence(ctx, queries.ChatEvidenceParams{OwnerID: owner, Did: did, ID: id})
	if errors.Is(e, pgx.ErrNoRows) {
		return out, router.NotFound("Message")
	}
	if e != nil {
		return out, e
	}
	out.Text, e = vault.Open(r.TextSealed)
	out.Sender = r.SenderDid
	out.Owner = r.SenderID
	return out, e
}
func DeleteOwner(ctx context.Context, tx pgx.Tx, owner string) error {
	q := queries.New(tx)
	if e := q.ChatLock(ctx, "chat-owner:"+owner); e != nil {
		return e
	}
	ids, e := q.ChatOwnerConversationIDs(ctx, owner)
	if e != nil {
		return e
	}
	for _, id := range ids {
		if e = q.ChatLock(ctx, "chat:"+id); e != nil {
			return e
		}
	}
	if e := q.ChatDeleteOwnMessages(ctx, owner); e != nil {
		return e
	}
	if e := q.ChatCloseOwnedConversations(ctx, owner); e != nil {
		return e
	}
	if e := q.ChatRemoveMember(ctx, owner); e != nil {
		return e
	}
	if e := q.ChatDeleteEmptyMessages(ctx); e != nil {
		return e
	}
	return q.ChatDeleteEmptyConversations(ctx)
}
func Register(services *lidza.Services) {
	jobs.FromServices(services).Handle("zing.chat-notify", func(ctx context.Context, raw json.RawMessage) error {
		var id string
		if e := json.Unmarshal(raw, &id); e != nil {
			return e
		}
		owners, e := queries.New(db.From(ctx)).ChatNotifyMembers(ctx, id)
		if e != nil {
			return e
		}
		for _, owner := range owners {
			if e = realtime.From(ctx).Publish(ctx, "user:"+owner, map[string]string{"kind": "chat-updated", "conversationId": id}); e != nil {
				return e
			}
		}
		return nil
	})
}
