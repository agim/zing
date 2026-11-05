package tests

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"zing/internal/providers"
	"zing/internal/social"

	"github.com/agim/lidza/packs/db"
	"github.com/agim/lidza/pkg/lidzatest"
	"zing/schema"
)

func TestHostedIdentityOwnershipRetriesAndSealedCredentials(t *testing.T) {
	var calls atomic.Int32
	var operation string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer fixture-agent-secret" {
			t.Error("agent auth missing")
		}
		var body struct{ ID, Username, Email, Password string }
		if e := json.NewDecoder(r.Body).Decode(&body); e != nil {
			t.Error(e)
			w.WriteHeader(400)
			return
		}
		if len(body.Password) < 24 || body.Username != "hostedperson" {
			t.Error("invalid generated identity")
		}
		if operation != "" && body.ID != operation {
			t.Error("retry changed operation ID")
		}
		operation = body.ID
		if calls.Add(1) == 1 {
			w.WriteHeader(502)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, e := w.Write([]byte(`{"subject":"actual-backend-id","handle":"hostedperson","access":"hosted-provider-token"}`))
		if e != nil {
			t.Error(e)
		}
	}))
	defer backend.Close()
	t.Setenv("ZING_ACTIVITYPUB_URL", backend.URL)
	t.Setenv("ZING_ACTIVITYPUB_AGENT_URL", backend.URL)
	t.Setenv("ZING_ACTIVITYPUB_AGENT_TOKEN", "fixture-agent-secret")
	s := setup(t, nil)
	owner, token := account(t, s, true)
	_, other := account(t, s, true)
	_, unverified := account(t, s, false)
	body := map[string]string{"network": "mastodon", "username": "hostedperson"}
	for _, entry := range []struct {
		token  string
		status int
	}{{"", 401}, {unverified, 403}} {
		if res := s.JSON(t, "POST", "/api/v1/hosting/identities", body, nil, lidzatest.Bearer(entry.token)); res.StatusCode != entry.status {
			t.Fatalf("authorization %d", res.StatusCode)
		}
	}
	if res := s.JSON(t, "POST", "/api/v1/hosting/identities", body, nil, lidzatest.Bearer(token)); res.StatusCode != 502 {
		t.Fatalf("provider failure %d", res.StatusCode)
	}
	var failed schema.HostingStatus
	s.JSON(t, "GET", "/api/v1/hosting", nil, &failed, lidzatest.Bearer(token))
	if len(failed.Identities) != 1 || failed.Identities[0].State != "failed" {
		t.Fatal("failed operation not persisted")
	}
	var active schema.HostedIdentityView
	if res := s.JSON(t, "POST", "/api/v1/hosting/identities", body, &active, lidzatest.Bearer(token)); res.StatusCode != 200 {
		t.Fatalf("retry %d", res.StatusCode)
	}
	if active.State != "active" || active.Handle != "hostedperson" {
		t.Fatal("identity not activated")
	}
	s.JSON(t, "POST", "/api/v1/hosting/identities", body, &active, lidzatest.Bearer(token))
	if calls.Load() != 2 {
		t.Fatal("active identity provisioned again")
	}
	if res := s.JSON(t, "POST", "/api/v1/hosting/identities", body, nil, lidzatest.Bearer(other)); res.StatusCode != 409 {
		t.Fatalf("username not reserved: %d", res.StatusCode)
	}
	var isolated schema.HostingStatus
	s.JSON(t, "GET", "/api/v1/hosting", nil, &isolated, lidzatest.Bearer(other))
	if len(isolated.Identities) != 0 {
		t.Fatal("another user's hosted identity leaked")
	}
	var connections schema.ConnectionList
	s.JSON(t, "GET", "/api/v1/connections", nil, &connections, lidzatest.Bearer(token))
	if len(connections.Items) != 1 || !connections.Items[0].Hosted {
		t.Fatal("hosted identity not connected")
	}
	if res := s.JSON(t, "DELETE", "/api/v1/connections/"+connections.Items[0].ID, nil, nil, lidzatest.Bearer(token)); res.StatusCode != 409 {
		t.Fatalf("hosted identity was disconnected: %d", res.StatusCode)
	}
	var password, access string
	if e := db.From(s.Context()).QueryRow(s.Context(), `SELECT h.password_sealed,c.access_sealed FROM hosted_identity h JOIN social_connection c ON c.owner_id=h.owner_id WHERE h.owner_id=$1`, owner).Scan(&password, &access); e != nil {
		t.Fatal(e)
	}
	if password == "" || strings.Contains(access, "hosted-provider-token") {
		t.Fatal("hosted secrets not sealed")
	}
	if res := s.JSON(t, "POST", "/api/v1/hosting/identities", map[string]string{"network": "mastodon", "username": "anothername"}, nil, lidzatest.Bearer(token)); res.StatusCode != 409 {
		t.Fatalf("identity renamed without migration: %d", res.StatusCode)
	}
	if res := s.JSON(t, "POST", "/api/v1/hosting/identities", map[string]string{"network": "threads", "username": "hostedperson"}, nil, lidzatest.Bearer(token)); res.StatusCode != 422 {
		t.Fatalf("unsupported hosting accepted: %d", res.StatusCode)
	}
}

func TestPDSRejectedNameCanChangeWithoutReplacingAnActiveIdentity(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/xrpc/com.atproto.server.createSession":
			w.WriteHeader(401)
			writeFixture(t, w, `{"error":"AuthenticationRequired"}`)
		case "/xrpc/com.atproto.server.createInviteCode":
			user, password, ok := r.BasicAuth()
			if !ok || user != "admin" || password != "fixture-admin" {
				t.Error("PDS administrative auth missing")
			}
			writeFixture(t, w, `{"code":"fixture-invite"}`)
		case "/xrpc/com.atproto.server.createAccount":
			var body struct {
				Handle string `json:"handle"`
			}
			if e := json.NewDecoder(r.Body).Decode(&body); e != nil {
				t.Error(e)
			}
			if body.Handle == "operator.test" {
				w.WriteHeader(400)
				writeFixture(t, w, `{"error":"HandleNotAvailable"}`)
				return
			}
			if body.Handle != "zingoperator.test" {
				t.Error("unexpected replacement handle")
			}
			writeFixture(t, w, `{"did":"did:plc:hosted-fixture","handle":"zingoperator.test","accessJwt":"fixture-access","refreshJwt":"fixture-refresh"}`)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer backend.Close()
	t.Setenv("ZING_PDS_URL", backend.URL)
	t.Setenv("ZING_PDS_ADMIN_PASSWORD", "fixture-admin")
	t.Setenv("ZING_PDS_HANDLE_DOMAIN", "test")
	s := setup(t, nil)
	_, token := account(t, s, true)
	if r := s.JSON(t, "POST", "/api/v1/hosting/identities", map[string]string{"network": "bluesky", "username": "operator"}, nil, lidzatest.Bearer(token)); r.StatusCode != 422 {
		t.Fatalf("reserved name %d", r.StatusCode)
	}
	var status schema.HostingStatus
	s.JSON(t, "GET", "/api/v1/hosting", nil, &status, lidzatest.Bearer(token))
	if len(status.Identities) != 1 || status.Identities[0].State != "rejected" {
		t.Fatal("definite rejection not recorded")
	}
	var identity schema.HostedIdentityView
	if r := s.JSON(t, "POST", "/api/v1/hosting/identities", map[string]string{"network": "bluesky", "username": "zingoperator"}, &identity, lidzatest.Bearer(token)); r.StatusCode != 200 {
		t.Fatalf("replacement rejected %d", r.StatusCode)
	}
	if identity.ID != status.Identities[0].ID || identity.State != "active" {
		t.Fatal("operation reservation replaced instead of recovered")
	}
	var connections schema.ConnectionList
	s.JSON(t, "GET", "/api/v1/connections", nil, &connections, lidzatest.Bearer(token))
	for _, c := range connections.Items {
		if c.Hosted {
			for _, capability := range c.Capabilities {
				if capability == "conversations" || capability == "notifications" {
					t.Fatal("PDS-only service falsely advertised")
				}
			}
		}
	}
}

func writeFixture(t *testing.T, w http.ResponseWriter, body string) {
	t.Helper()
	if _, e := w.Write([]byte(body)); e != nil {
		t.Error(e)
	}
}

func TestHostedAccountDeletionUsesDurableSealedCleanupJob(t *testing.T) {
	var deleted atomic.Int32
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/accounts/delete" || r.Header.Get("Authorization") != "Bearer hosted-delete-token" {
			t.Error("incorrect deletion authorization")
			w.WriteHeader(403)
			return
		}
		if e := r.ParseForm(); e != nil {
			t.Error(e)
		}
		if r.Form.Get("password") != "generated-backend-password" {
			t.Error("deletion used the Zing password instead of backend credentials")
		}
		deleted.Add(1)
		w.WriteHeader(202)
	}))
	defer backend.Close()
	t.Setenv("ZING_ACTIVITYPUB_URL", backend.URL)
	s := setup(t, nil)
	owner, token := account(t, s, true)
	if e := social.SaveConnection(s.Context(), owner, providers.Session{Network: "mastodon", Server: backend.URL, Subject: "backend-delete-id", Handle: "deleteme", Access: "hosted-delete-token"}); e != nil {
		t.Fatal(e)
	}
	password, e := social.Seal("generated-backend-password")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = db.From(s.Context()).Exec(s.Context(), `UPDATE social_connection SET hosted=true WHERE owner_id=$1`, owner); e != nil {
		t.Fatal(e)
	}
	if _, e = db.From(s.Context()).Exec(s.Context(), `INSERT INTO hosted_identity(owner_id,network,username,server,subject,password_sealed,state) VALUES($1,'mastodon',$1,$2,'backend-delete-id',$3,'active')`, owner, backend.URL, password); e != nil {
		t.Fatal(e)
	}
	if r := s.JSON(t, "POST", "/api/v1/auth/delete", map[string]string{"password": "test-password-123"}, nil, lidzatest.Bearer(token)); r.StatusCode != 204 {
		t.Fatalf("deletion %d", r.StatusCode)
	}
	for i := 0; i < 50 && deleted.Load() == 0; i++ {
		time.Sleep(100 * time.Millisecond)
	}
	if deleted.Load() != 1 {
		t.Fatal("hosted account deletion was not delivered")
	}
	var count int
	if e = db.From(s.Context()).QueryRow(s.Context(), `SELECT count(*) FROM hosted_identity WHERE owner_id=$1`, owner).Scan(&count); e != nil || count != 0 {
		t.Fatalf("hosted local row survived: %d %v", count, e)
	}
	var payload string
	if e = db.From(s.Context()).QueryRow(s.Context(), `SELECT payload::text FROM job WHERE kind='zing.delete-hosted' AND payload->>'Subject'='backend-delete-id' ORDER BY created_at DESC LIMIT 1`).Scan(&payload); e != nil {
		t.Fatal(e)
	}
	if strings.Contains(payload, "hosted-delete-token") || strings.Contains(payload, "generated-backend-password") {
		t.Fatal("cleanup job retained plaintext credentials")
	}
}
