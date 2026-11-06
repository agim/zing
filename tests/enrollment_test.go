package tests

import (
	"encoding/json"
	"github.com/agim/lidza/packs/db"
	"github.com/agim/lidza/pkg/lidzatest"
	"github.com/google/uuid"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"zing/schema"
)

func TestSignupAutomaticallyProvisionsVerifiedIdentitiesAndRecoversPartialFailure(t *testing.T) {
	var fail, exists atomic.Bool
	fail.Store(true)
	var ap, created atomic.Int32
	var handle string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/provision":
			if r.Header.Get("Authorization") != "Bearer enrollment-agent" {
				t.Error("missing provisioning authorization")
			}
			var body map[string]string
			if e := json.NewDecoder(r.Body).Decode(&body); e != nil {
				t.Error(e)
			}
			if strings.Contains(body["email"], "@zing.example") {
				t.Error("Zing email copied into ActivityPub")
			}
			ap.Add(1)
			json.NewEncoder(w).Encode(map[string]string{"subject": "enrolled-native-ap", "handle": body["username"], "access": "native-ap-secret"})
		case "/xrpc/com.atproto.server.createSession":
			if !exists.Load() {
				w.WriteHeader(401)
				w.Write([]byte(`{"error":"AuthenticationRequired"}`))
				return
			}
			json.NewEncoder(w).Encode(map[string]string{"did": "did:plc:enrollednativefixture1234", "handle": handle, "accessJwt": "native-bsky-secret", "refreshJwt": "native-refresh-secret"})
		case "/xrpc/com.atproto.server.createInviteCode":
			u, p, ok := r.BasicAuth()
			if !ok || u != "admin" || p != "enrollment-admin" {
				t.Error("missing PDS admin credentials")
			}
			w.Write([]byte(`{"code":"fixture-invite"}`))
		case "/xrpc/com.atproto.server.createAccount":
			if fail.Load() {
				w.WriteHeader(503)
				w.Write([]byte(`{"error":"FixtureUnavailable"}`))
				return
			}
			var body map[string]string
			if e := json.NewDecoder(r.Body).Decode(&body); e != nil {
				t.Error(e)
				w.WriteHeader(400)
				return
			}
			handle = body["handle"]
			if len(strings.TrimSuffix(handle, ".test")) > 18 {
				t.Error("PDS service handle exceeds native limit")
				w.WriteHeader(400)
				return
			}
			created.Add(1)
			exists.Store(true)
			json.NewEncoder(w).Encode(map[string]string{"did": "did:plc:enrollednativefixture1234", "handle": handle, "accessJwt": "native-bsky-secret", "refreshJwt": "native-refresh-secret"})
		default:
			w.WriteHeader(404)
		}
	}))
	defer backend.Close()
	t.Setenv("ZING_ACTIVITYPUB_URL", backend.URL)
	t.Setenv("ZING_ACTIVITYPUB_AGENT_URL", backend.URL)
	t.Setenv("ZING_ACTIVITYPUB_AGENT_TOKEN", "enrollment-agent")
	t.Setenv("ZING_PDS_URL", backend.URL)
	t.Setenv("ZING_PDS_ADMIN_PASSWORD", "enrollment-admin")
	t.Setenv("ZING_PDS_HANDLE_DOMAIN", "test")
	s := setup(t, nil)
	email := uuid.NewString() + "@zing.example"
	var signed struct{ Subject, AccessToken string }
	if r := s.JSON(t, "POST", "/api/v1/auth/register", map[string]string{"email": email, "name": "Unified signup fixture", "password": "signup-password-123"}, &signed); r.StatusCode != 201 {
		t.Fatal("signup", r.StatusCode)
	}
	if signed.Subject == "" {
		t.Fatal("signup subject absent")
	}
	for i := 0; i < 80; i++ {
		var state string
		e := db.From(s.Context()).QueryRow(s.Context(), `SELECT state FROM job WHERE kind='zing.enrollment' AND payload#>>'{}'=$1 ORDER BY created_at DESC LIMIT 1`, signed.Subject).Scan(&state)
		if e == nil && state == "done" {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if ap.Load() != 0 || created.Load() != 0 {
		t.Fatal("unverified signup reached native servers")
	}
	var enrollmentName, body string
	if e := db.From(s.Context()).QueryRow(s.Context(), `SELECT username FROM hosted_enrollment WHERE owner_id=$1`, signed.Subject).Scan(&enrollmentName); e != nil {
		t.Fatal(e)
	}
	if !regexp.MustCompile(`^[a-z0-9]{1,30}$`).MatchString(enrollmentName) || len(enrollmentName) > 18 || len(enrollmentName) < 3 {
		t.Fatal("common username invalid")
	}
	if e := db.From(s.Context()).QueryRow(s.Context(), `SELECT coalesce(text,'')||coalesce(html,'') FROM mail_message WHERE recipient=$1 ORDER BY created_at DESC LIMIT 1`, email).Scan(&body); e != nil {
		t.Fatal(e)
	}
	token := regexp.MustCompile(`[?&]token=([A-Za-z0-9_-]+)`).FindStringSubmatch(body)
	if len(token) != 2 {
		t.Fatal("verification mail absent")
	}
	if r := s.JSON(t, "POST", "/api/v1/auth/verify", map[string]string{"token": token[1]}, nil, lidzatest.Bearer(signed.AccessToken)); r.StatusCode != 204 {
		t.Fatal("verify", r.StatusCode)
	}
	var active, failed int
	for i := 0; i < 100; i++ {
		e := db.From(s.Context()).QueryRow(s.Context(), `SELECT count(*) FILTER(WHERE state='active'),count(*) FILTER(WHERE state='failed') FROM hosted_identity WHERE owner_id=$1`, signed.Subject).Scan(&active, &failed)
		if e != nil {
			t.Fatal(e)
		}
		if active == 1 && failed == 1 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if active != 1 || failed != 1 {
		t.Fatal("partial failure not persisted", active, failed)
	}
	var status schema.HostingStatus
	if r := s.JSON(t, "GET", "/api/v1/hosting", nil, &status, lidzatest.Bearer(signed.AccessToken)); r.StatusCode != 200 || status.Enrollment == nil || status.Enrollment.State != "partial" {
		t.Fatal("partial status absent", r.StatusCode, status.Enrollment)
	}
	fail.Store(false)
	if r := s.JSON(t, "POST", "/api/v1/hosting/retry", nil, nil, lidzatest.Bearer(signed.AccessToken)); r.StatusCode != 200 {
		t.Fatal("retry", r.StatusCode)
	}
	for i := 0; i < 150; i++ {
		if e := db.From(s.Context()).QueryRow(s.Context(), `SELECT count(*) FROM hosted_identity WHERE owner_id=$1 AND state='active'`, signed.Subject).Scan(&active); e != nil {
			t.Fatal(e)
		}
		if active == 2 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if active != 2 || created.Load() != 1 || ap.Load() != 1 {
		t.Fatal("recovery duplicated or lost an identity", active, created.Load(), ap.Load())
	}
	for range 2 {
		if r := s.JSON(t, "POST", "/api/v1/hosting/retry", nil, nil, lidzatest.Bearer(signed.AccessToken)); r.StatusCode != 200 {
			t.Fatal("repeated retry", r.StatusCode)
		}
	}
	if r := s.JSON(t, "GET", "/api/v1/hosting", nil, &status, lidzatest.Bearer(signed.AccessToken)); r.StatusCode != 200 || status.Enrollment.State != "active" || len(status.Identities) != 2 {
		t.Fatal("unified ready state", r.StatusCode, status)
	}
	_, other := account(t, s, true)
	if r := s.JSON(t, "POST", "/api/v1/hosting/retry", nil, nil, lidzatest.Bearer(other)); r.StatusCode != 404 {
		t.Fatal("unrelated user reached enrollment", r.StatusCode)
	}
	var count int
	if e := db.From(s.Context()).QueryRow(s.Context(), `SELECT count(*) FROM social_connection WHERE owner_id=$1 AND hosted`, signed.Subject).Scan(&count); e != nil || count != 2 {
		t.Fatal("hosted connections absent", count, e)
	}
}
