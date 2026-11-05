package social

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"github.com/agim/lidza/packs/db"
	"github.com/agim/lidza/pkg/router"
	"sort"
	"strings"
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
		if req.Query("connectionID") != "" && req.Query("connectionID") != c.ID {
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
	s, e := Load(ctx, Owner(ctx), req.Query("connectionID"))
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
		items, err := s.Conversations(ctx)
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
	s, e := Load(ctx, Owner(ctx), req.Query("connectionID"))
	if e != nil {
		return out, e
	}
	out.Items, e = s.Messages(ctx, req.Query("conversationID"))
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
	e = s.SendMessage(ctx, req.Body)
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
	v, e := s.OpenConversation(ctx, req.Body.Recipient)
	v.ConnectionID = req.Body.ConnectionID
	v.Network = s.Network
	return v, e
}
