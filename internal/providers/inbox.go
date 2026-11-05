package providers

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"zing/schema"
)

func (s Session) Notifications(ctx context.Context) ([]schema.NotificationView, error) {
	items := []schema.NotificationView{}
	if s.LocalView && s.Network == "bluesky" {
		return items, Unsupported()
	}
	switch s.Network {
	case "mastodon":
		var list []struct {
			ID        string   `json:"id"`
			Type      string   `json:"type"`
			CreatedAt string   `json:"created_at"`
			Account   MAccount `json:"account"`
			Status    *MPost   `json:"status"`
		}
		if e := s.Do(ctx, "GET", "/api/v1/notifications?limit=30", nil, &list); e != nil {
			return items, e
		}
		for _, n := range list {
			t := n.Account.Acct + " · " + n.Type
			if n.Status != nil {
				t += " · " + Plain(n.Status.Content)
			}
			items = append(items, schema.NotificationView{ID: n.ID, Kind: n.Type, Text: t, CreatedAt: n.CreatedAt})
		}
	case "bluesky":
		var reply struct {
			Notifications []struct {
				URI       string   `json:"uri"`
				Reason    string   `json:"reason"`
				IndexedAt string   `json:"indexedAt"`
				Author    BAccount `json:"author"`
			} `json:"notifications"`
		}
		if e := s.Do(ctx, "GET", "/xrpc/app.bsky.notification.listNotifications?limit=30", nil, &reply); e != nil {
			return items, e
		}
		for _, n := range reply.Notifications {
			items = append(items, schema.NotificationView{ID: n.URI, Kind: n.Reason, Text: n.Author.Handle + " · " + n.Reason, CreatedAt: n.IndexedAt})
		}
	default:
		return items, Unsupported()
	}
	return items, nil
}
func (s Session) chat() Session { c := s; c.Proxy = "did:web:api.bsky.chat#bsky_chat"; return c }
func (s Session) Conversations(ctx context.Context) ([]schema.ConversationView, error) {
	items := []schema.ConversationView{}
	if s.LocalView && s.Network == "bluesky" {
		return items, Unsupported()
	}
	switch s.Network {
	case "mastodon":
		var list []struct {
			ID         string     `json:"id"`
			Unread     bool       `json:"unread"`
			Accounts   []MAccount `json:"accounts"`
			LastStatus *MPost     `json:"last_status"`
		}
		if e := s.Do(ctx, "GET", "/api/v1/conversations?limit=30", nil, &list); e != nil {
			return items, e
		}
		for _, c := range list {
			names := []string{}
			for _, a := range c.Accounts {
				names = append(names, "@"+a.Acct)
			}
			v := schema.ConversationView{ID: c.ID, Title: strings.Join(names, ", "), Recipient: strings.Join(names, " "), Unread: c.Unread}
			if c.LastStatus != nil {
				v.LastText = Plain(c.LastStatus.Content)
				v.ID = c.LastStatus.ID
			}
			items = append(items, v)
		}
	case "bluesky":
		var reply struct {
			Convos []struct {
				ID          string     `json:"id"`
				Unread      int        `json:"unreadCount"`
				Members     []BAccount `json:"members"`
				LastMessage struct {
					Text string `json:"text"`
				} `json:"lastMessage"`
			} `json:"convos"`
		}
		if e := s.chat().Do(ctx, "GET", "/xrpc/chat.bsky.convo.listConvos?limit=30", nil, &reply); e != nil {
			return items, e
		}
		for _, c := range reply.Convos {
			names := []string{}
			for _, m := range c.Members {
				if m.DID != s.Subject {
					names = append(names, m.Handle)
				}
			}
			items = append(items, schema.ConversationView{ID: c.ID, Title: strings.Join(names, ", "), LastText: c.LastMessage.Text, Unread: c.Unread > 0})
		}
	default:
		return items, Unsupported()
	}
	return items, nil
}
func (s Session) Messages(ctx context.Context, id string) ([]schema.MessageView, error) {
	items := []schema.MessageView{}
	if s.Network == "mastodon" && strings.HasPrefix(id, "new:") {
		return items, nil
	}
	if s.LocalView && s.Network == "bluesky" {
		return items, Unsupported()
	}
	switch s.Network {
	case "mastodon":
		var root MPost
		if e := s.Do(ctx, "GET", "/api/v1/statuses/"+url.PathEscape(id), nil, &root); e != nil {
			return items, e
		}
		if root.Visibility != "direct" {
			return items, Unsupported()
		}
		var thread struct {
			Ancestors   []MPost `json:"ancestors"`
			Descendants []MPost `json:"descendants"`
		}
		if e := s.Do(ctx, "GET", "/api/v1/statuses/"+url.PathEscape(id)+"/context", nil, &thread); e != nil {
			return items, e
		}
		all := append(thread.Ancestors, root)
		all = append(all, thread.Descendants...)
		for _, p := range all {
			if p.Visibility == "direct" {
				items = append(items, schema.MessageView{ID: p.ID, Sender: p.Account.Acct, Text: Plain(p.Content), CreatedAt: p.CreatedAt})
			}
		}
	case "bluesky":
		var reply struct {
			Messages []struct {
				ID     string `json:"id"`
				Text   string `json:"text"`
				SentAt string `json:"sentAt"`
				Sender struct {
					DID string `json:"did"`
				} `json:"sender"`
			} `json:"messages"`
		}
		if e := s.chat().Do(ctx, "GET", "/xrpc/chat.bsky.convo.getMessages?convoID="+url.QueryEscape(id)+"&limit=50", nil, &reply); e != nil {
			return items, e
		}
		for i := len(reply.Messages) - 1; i >= 0; i-- {
			m := reply.Messages[i]
			items = append(items, schema.MessageView{ID: m.ID, Sender: m.Sender.DID, Text: m.Text, CreatedAt: m.SentAt})
		}
	default:
		return items, Unsupported()
	}
	return items, nil
}
func (s Session) SendMessage(ctx context.Context, in schema.MessageInput) error {
	if s.LocalView && s.Network == "bluesky" {
		return Unsupported()
	}
	switch s.Network {
	case "bluesky":
		return s.chat().Do(ctx, "POST", "/xrpc/chat.bsky.convo.sendMessage", map[string]any{"convoID": in.ConversationID, "message": map[string]string{"text": in.Text}}, nil)
	case "mastodon":
		if strings.HasPrefix(in.ConversationID, "new:") {
			if strings.Contains(in.Text, "@") {
				return Unsupported()
			}
			person, e := s.Profile(ctx, strings.TrimPrefix(in.ConversationID, "new:"))
			if e != nil {
				return e
			}
			return s.Do(ctx, "POST", "/api/v1/statuses", map[string]string{"status": "@" + person.Handle + " " + in.Text, "visibility": "direct"}, nil)
		}
		// The recipient is taken from the server's existing private conversation,
		// never from a client-provided address. Refuse extra mentions.
		var p MPost
		if e := s.Do(ctx, "GET", "/api/v1/statuses/"+url.PathEscape(in.ConversationID), nil, &p); e != nil {
			return e
		}
		if p.Visibility != "direct" {
			return Unsupported()
		}
		if strings.Contains(in.Text, "@") {
			return fmt.Errorf("private replies cannot add recipients")
		}
		var raw struct {
			Mentions []struct {
				Acct string `json:"acct"`
			} `json:"mentions"`
			Account MAccount `json:"account"`
		}
		if e := s.Do(ctx, "GET", "/api/v1/statuses/"+url.PathEscape(in.ConversationID), nil, &raw); e != nil {
			return e
		}
		names := []string{}
		seen := map[string]bool{s.Handle: true}
		for _, a := range append(raw.Mentions, struct {
			Acct string `json:"acct"`
		}{Acct: raw.Account.Acct}) {
			if !seen[a.Acct] {
				names = append(names, "@"+a.Acct)
				seen[a.Acct] = true
			}
		}
		if len(names) == 0 {
			return Unsupported()
		}
		return s.Do(ctx, "POST", "/api/v1/statuses", map[string]string{"status": strings.Join(names, " ") + " " + in.Text, "visibility": "direct", "in_reply_to_id": in.ConversationID}, nil)
	}
	return Unsupported()
}

func (s Session) OpenConversation(ctx context.Context, recipient string) (schema.ConversationView, error) {
	if s.LocalView && s.Network == "bluesky" {
		return schema.ConversationView{}, Unsupported()
	}
	if s.Network == "bluesky" {
		var result struct {
			Convo struct {
				ID string `json:"id"`
			} `json:"convo"`
		}
		if e := s.chat().Do(ctx, "GET", "/xrpc/chat.bsky.convo.getConvoForMembers?members="+url.QueryEscape(recipient), nil, &result); e != nil {
			return schema.ConversationView{}, e
		}
		p, e := s.Profile(ctx, recipient)
		return schema.ConversationView{ID: result.Convo.ID, Title: p.Handle, Recipient: recipient}, e
	}
	if s.Network == "mastodon" {
		p, e := s.Profile(ctx, recipient)
		return schema.ConversationView{ID: "new:" + p.ID, Title: p.Handle, Recipient: p.ID}, e
	}
	return schema.ConversationView{}, Unsupported()
}
