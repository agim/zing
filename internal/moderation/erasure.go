package moderation

import (
	"context"
	"errors"
	"github.com/agim/lidza/pkg/env"
	"net/url"
)

// ConfirmActivityPubErasure handles accounts whose disabled native sessions
// cannot invoke self-deletion. Native suspension is destructive, so this path
// is exclusively for an already authenticated account-deletion request.
func ConfirmActivityPubErasure(ctx context.Context, origin, subject, username, operation string, selfDeleteAccepted bool) error {
	v, e := env.Values(".")
	if e != nil {
		return e
	}
	if v["ZING_ACTIVITYPUB_AGENT_URL"] == "" {
		if selfDeleteAccepted {
			return nil
		}
		return errors.New("native account erasure needs administration")
	}
	var state struct {
		Erased bool `json:"erased"`
	}
	if e = agent(ctx, "/account-erased", map[string]string{"id": operation, "subject": subject, "username": username}, &state); e != nil {
		return e
	}
	if !state.Erased {
		if !selfDeleteAccepted {
			token, err := ActivityPubAdmin(ctx, origin)
			if err != nil {
				return err
			}
			if e = backend(ctx, origin, "POST", "/api/v1/admin/accounts/"+url.PathEscape(subject)+"/action", token, map[string]string{"type": "suspend", "text": "User requested account erasure"}, false, nil); e != nil {
				return e
			}
		}
		return errors.New("native erasure is still processing; verify again before completing cleanup")
	}
	return agent(ctx, "/forget", map[string]string{"id": operation, "username": username}, nil)
}
