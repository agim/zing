package social

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"github.com/agim/lidza/packs/db"
	"github.com/agim/lidza/pkg/router"
	"sort"
	"strings"
	queries "zing/db/queries/gen"
	"zing/internal/atview"
	"zing/internal/chat"
	"zing/internal/providers"
	"zing/schema"
)

func Feed(ctx context.Context, req *router.Request[router.None]) (schema.FeedPage, error) {
	out := schema.FeedPage{Items: []schema.SocialPost{}, Warnings: []string{}}
	conns, e := Connections(ctx, nil)
	if e != nil {
		return out, e
	}
	cursors := map[string]string{}
	if req.Query("cursor") != "" {
		b, err := base64.RawURLEncoding.DecodeString(req.Query("cursor"))
		if err != nil || len(b) > 8192 {
			return out, router.Errorf(422, "Invalid cursor")
		}
		if err = json.Unmarshal(b, &cursors); err != nil {
			return out, router.Errorf(422, "Invalid cursor")
		}
	}
	next := map[string]string{}
	for _, c := range conns.Items {
		if queryID(req, "connection") != "" && queryID(req, "connection") != c.ID {
			continue
		}
		if req.Query("network") != "" && req.Query("network") != c.Network {
			continue
		}
		if cursors[c.ID] == "~end" {
			next[c.ID] = "~end"
			continue
		}
		s, err := Load(ctx, Owner(ctx), c.ID)
		if err != nil {
			out.Warnings = append(out.Warnings, c.Network+": reconnect required")
			continue
		}
		if s.LocalView {
			s.View = atview.Reader{Owner: Owner(ctx), DID: s.Subject, Server: s.Server, Scope: req.Query("scope")}
			if syncErr := atview.Refresh(ctx, s); syncErr != nil {
				out.Warnings = append(out.Warnings, "Hosted Bluesky synchronization failed; retained indexed records are shown.")
			}
		}
		f, err := s.Feed(ctx, cursors[c.ID], req.Query("q"))
		if err != nil {
			out.Warnings = append(out.Warnings, c.Network+": "+publicError(err))
			continue
		}
		for _, p := range f.Items {
			p.ConnectionID = c.ID
			p.Network = c.Network
			var mapped string
			err = db.From(ctx).QueryRow(ctx, `SELECT draft_id::text FROM delivery WHERE owner_id=$1 AND connection_id::text=$2 AND remote_id=$3 AND state='published' LIMIT 1`, Owner(ctx), c.ID, p.ID).Scan(&mapped)
			if err == nil {
				p.RelayParentDraftID = mapped
			}
			out.Items = append(out.Items, p)
		}
		out.Warnings = append(out.Warnings, f.Warnings...)
		if f.Cursor != "" && len(f.Items) > 0 {
			next[c.ID] = f.Cursor
		} else {
			next[c.ID] = "~end"
		}
	}
	sort.SliceStable(out.Items, func(i, j int) bool { return out.Items[i].CreatedAt > out.Items[j].CreatedAt })
	hasNext := false
	for _, v := range next {
		if v != "~end" {
			hasNext = true
		}
	}
	if hasNext {
		b, err := json.Marshal(next)
		if err != nil {
			return out, err
		}
		out.Cursor = base64.RawURLEncoding.EncodeToString(b)
	}
	return out, nil
}
func publicError(e error) string {
	if h, ok := e.(*router.HTTPError); ok {
		return h.Message
	}
	return "The network is temporarily unavailable"
}
func Profile(ctx context.Context, req *router.Request[router.None]) (schema.SocialAccount, error) {
	s, e := Load(ctx, Owner(ctx), queryID(req, "connection"))
	if e != nil {
		return schema.SocialAccount{}, e
	}
	return s.Profile(ctx, req.Query("id"))
}
func Action(ctx context.Context, req *router.Request[schema.ActionRequest]) (schema.OperationResult, error) {
	s, e := Load(ctx, Owner(ctx), req.Body.ConnectionID)
	if e != nil {
		return schema.OperationResult{}, e
	}
	var in schema.ActionInput
	if e = Convert(req.Body, &in); e != nil {
		return schema.OperationResult{}, e
	}
	if s.LocalView {
		if in.Action == "mute" || in.Action == "unmute" {
			e = atview.Mute(ctx, Owner(ctx), s.Subject, in.Target, in.Action == "mute")
			return schema.OperationResult{Ok: e == nil}, e
		}
		tx, err := db.From(ctx).Begin(ctx)
		if err != nil {
			return schema.OperationResult{}, err
		}
		defer tx.Rollback(ctx)
		if err = queries.New(tx).AtSyncLock(ctx, "at-action:"+s.Subject); err != nil {
			return schema.OperationResult{}, err
		}
		present, err := atview.IsActionPresent(ctx, s.Subject, in.Action, in.Target)
		if err != nil {
			return schema.OperationResult{}, err
		}
		if present {
			return schema.OperationResult{Ok: true}, nil
		}
		var written providers.Ref
		s.Written = &written
		if e = s.Action(ctx, in); e != nil {
			return schema.OperationResult{}, e
		}
		if written.URI != "" {
			e = atview.RefreshOne(ctx, s, written.URI)
		} else {
			e = atview.RemoveAction(ctx, s.Subject, in.Action, in.Target)
		}
		if e != nil {
			return schema.OperationResult{}, e
		}
		if e = tx.Commit(ctx); e != nil {
			return schema.OperationResult{}, e
		}
		return schema.OperationResult{Ok: true}, nil
	}
	e = s.Action(ctx, in)
	return schema.OperationResult{Ok: e == nil}, e
}
func Notifications(ctx context.Context, req *router.Request[router.None]) (schema.NotificationList, error) {
	out := schema.NotificationList{Items: []schema.NotificationView{}, Warnings: []string{}}
	conns, e := Connections(ctx, nil)
	if e != nil {
		return out, e
	}
	for _, c := range conns.Items {
		if c.Network == "threads" {
			continue
		}
		s, err := Load(ctx, Owner(ctx), c.ID)
		if err != nil {
			out.Warnings = append(out.Warnings, c.Network+": reconnect required")
			continue
		}
		items, err := s.Notifications(ctx)
		if err != nil {
			out.Warnings = append(out.Warnings, c.Network+": "+publicError(err))
			continue
		}
		for _, v := range items {
			v.ConnectionID = c.ID
			v.Network = c.Network
			out.Items = append(out.Items, v)
		}
	}
	return out, nil
}
func Conversations(ctx context.Context, req *router.Request[router.None]) (schema.ConversationList, error) {
	out := schema.ConversationList{Items: []schema.ConversationView{}, Warnings: []string{}}
	conns, e := Connections(ctx, nil)
	if e != nil {
		return out, e
	}
	for _, c := range conns.Items {
		if c.Network == "threads" {
			continue
		}
		s, err := Load(ctx, Owner(ctx), c.ID)
		if err != nil {
			out.Warnings = append(out.Warnings, c.Network+": reconnect required")
			continue
		}
		var items []schema.ConversationView
		if s.Hosted && s.Network == "bluesky" {
			items, err = chat.List(ctx, Owner(ctx), s.Subject)
		} else {
			items, err = s.Conversations(ctx)
		}
		if err != nil {
			out.Warnings = append(out.Warnings, c.Network+": "+publicError(err))
			continue
		}
		for _, v := range items {
			v.ConnectionID = c.ID
			v.Network = c.Network
			out.Items = append(out.Items, v)
		}
	}
	return out, nil
}
func Messages(ctx context.Context, req *router.Request[router.None]) (schema.MessageList, error) {
	out := schema.MessageList{Items: []schema.MessageView{}}
	s, e := Load(ctx, Owner(ctx), queryID(req, "connection"))
	if e != nil {
		return out, e
	}
	if s.Hosted && s.Network == "bluesky" {
		return chat.Messages(ctx, Owner(ctx), s.Subject, queryID(req, "conversation"), req.Query("cursor"))
	}
	out.Items, e = s.Messages(ctx, queryID(req, "conversation"))
	return out, e
}
func SendMessage(ctx context.Context, req *router.Request[schema.MessageInput]) (schema.OperationResult, error) {
	if strings.TrimSpace(req.Body.Text) == "" {
		return schema.OperationResult{}, router.Errorf(422, "Write a message")
	}
	s, e := Load(ctx, Owner(ctx), req.Body.ConnectionID)
	if e != nil {
		return schema.OperationResult{}, e
	}
	if s.Hosted && s.Network == "bluesky" {
		e = chat.Send(ctx, Owner(ctx), s.Subject, req.Body)
	} else {
		e = s.SendMessage(ctx, req.Body)
	}
	return schema.OperationResult{Ok: e == nil}, e
}

func Search(ctx context.Context, req *router.Request[router.None]) (schema.FeedPage, error) {
	return Feed(ctx, req)
}

func People(ctx context.Context, req *router.Request[router.None]) (schema.PeoplePage, error) {
	out := schema.PeoplePage{Items: []schema.PersonResult{}, Warnings: []string{}}
	if strings.TrimSpace(req.Query("q")) == "" {
		return out, nil
	}
	conns, e := Connections(ctx, nil)
	if e != nil {
		return out, e
	}
	for _, c := range conns.Items {
		if req.Query("network") != "" && c.Network != req.Query("network") {
			continue
		}
		if c.Network == "threads" {
			out.Warnings = append(out.Warnings, "Threads people search is unavailable through this integration.")
			continue
		}
		s, err := Load(ctx, Owner(ctx), c.ID)
		if err != nil {
			out.Warnings = append(out.Warnings, c.Network+": reconnect required")
			continue
		}
		items, err := s.People(ctx, req.Query("q"))
		if err != nil {
			out.Warnings = append(out.Warnings, c.Network+": "+publicError(err))
			continue
		}
		for _, p := range items {
			out.Items = append(out.Items, schema.PersonResult{ConnectionID: c.ID, Network: c.Network, Profile: p})
		}
	}
	return out, nil
}
func OpenConversation(ctx context.Context, req *router.Request[schema.OpenConversationRequest]) (schema.ConversationView, error) {
	s, e := Load(ctx, Owner(ctx), req.Body.ConnectionID)
	if e != nil {
		return schema.ConversationView{}, e
	}
	var v schema.ConversationView
	if s.Hosted && s.Network == "bluesky" {
		v, e = chat.Open(ctx, Owner(ctx), s.Subject, req.Body.Recipient)
	} else {
		v, e = s.OpenConversation(ctx, req.Body.Recipient)
	}
	v.ConnectionID = req.Body.ConnectionID
	v.Network = s.Network
	return v, e
}

func AcceptZingConversation(ctx context.Context, req *router.Request[schema.ChatAcceptRequest]) (schema.OperationResult, error) {
	s, e := Load(ctx, Owner(ctx), req.Body.ConnectionID)
	if e != nil {
		return schema.OperationResult{}, e
	}
	if !s.Hosted || s.Network != "bluesky" {
		return schema.OperationResult{}, providers.Unsupported()
	}
	e = chat.Accept(ctx, Owner(ctx), s.Subject, req.Param("id"))
	return schema.OperationResult{Ok: e == nil}, e
}

// The generated web client uses camelCase. Retain the initial uppercase-ID
// query spelling for existing clients while checking ownership identically.
func queryID(req *router.Request[router.None], name string) string {
	if id := req.Query(name + "Id"); id != "" {
		return id
	}
	return req.Query(name + "ID")
}
