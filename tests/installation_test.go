package tests

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/agim/lidza/packs/auth"
	"github.com/agim/lidza/packs/db"
	"github.com/agim/lidza/pkg/credentials"
	"github.com/agim/lidza/pkg/lidzatest"
	"zing/schema"
)

func TestPrivateInstanceRejectsExistingOutsidersAndNewRegistrations(t *testing.T) {
	s := setup(t, nil)
	owner, ownerToken := account(t, s, true)
	guest, guestToken := account(t, s, true)
	t.Setenv("ZING_ACCESS_MODE", "private")
	t.Setenv("ADMIN_USERS", owner)
	if r := s.JSON(t, "GET", "/api/v1/connections", nil, nil, lidzatest.Bearer(ownerToken)); r.StatusCode != 200 {
		t.Fatalf("owner denied %d", r.StatusCode)
	}
	if r := s.JSON(t, "GET", "/api/v1/connections", nil, nil, lidzatest.Bearer(guestToken)); r.StatusCode != 403 {
		t.Fatalf("outsider admitted %d", r.StatusCode)
	}
	if r := s.JSON(t, "POST", "/api/v1/auth/register", map[string]string{"email": "outsider-new@zing.example", "password": "test-password-123"}, nil); r.StatusCode != 403 {
		t.Fatalf("private registration admitted %d", r.StatusCode)
	}
	profile, e := auth.From(s.Context()).Profile(s.Context(), guest)
	if e != nil {
		t.Fatal(e)
	}
	t.Setenv("ZING_ALLOWED_EMAILS", profile.Email)
	if r := s.JSON(t, "GET", "/api/v1/connections", nil, nil, lidzatest.Bearer(guestToken)); r.StatusCode != 200 {
		t.Fatalf("allowed user denied %d", r.StatusCode)
	}
}
func TestInstallationUsesEncryptedDatabaseSettingsAndAdminGate(t *testing.T) {
	s := setup(t, nil)
	owner, ownerToken := account(t, s, true)
	_, guestToken := account(t, s, true)
	t.Setenv("ADMIN_USERS", owner)
	if r := s.JSON(t, "POST", "/admin/installation/complete", nil, nil, lidzatest.Bearer(guestToken)); r.StatusCode != 403 {
		t.Fatalf("ordinary user completed setup %d", r.StatusCode)
	}
	for name, value := range map[string]string{"APP_URL": "https://selfhost.example.org", "ZING_ACCESS_MODE": "private", "MAIL_PROVIDER": "smtp", "MAIL_FROM": "owner@selfhost.example.org", "MAIL_SMTP_URL": "smtp://fixture:fixture-password@mail.example.org:587", "STORAGE_PROVIDER": "local", "STORAGE_DIR": t.TempDir()} {
		t.Setenv(name, value)
	}
	req, e := http.NewRequest("POST", s.URL+"/admin/installation/complete", strings.NewReader(""))
	if e != nil {
		t.Fatal(e)
	}
	req.Header.Set("Authorization", "Bearer "+ownerToken)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, e := s.Client().Do(req)
	if e != nil {
		t.Fatal(e)
	}
	body, e := io.ReadAll(res.Body)
	res.Body.Close()
	if e != nil {
		t.Fatal(e)
	}
	if res.StatusCode != 200 || !strings.Contains(string(body), "saved encrypted in PostgreSQL") {
		t.Fatalf("setup did not render confirmation: %d %s", res.StatusCode, body)
	}
	if strings.Contains(string(body), "fixture-password") {
		t.Fatal("secret returned in operator page")
	}
	var sealed string
	if e = db.From(s.Context()).QueryRow(s.Context(), `SELECT sealed FROM credential WHERE name='MAIL_SMTP_URL'`).Scan(&sealed); e != nil {
		t.Fatal(e)
	}
	if strings.Contains(sealed, "fixture-password") {
		t.Fatal("SMTP credentials stored in plaintext")
	}
	key, e := credentials.Key(".")
	if e != nil {
		t.Fatal(e)
	}
	plain, e := credentials.Decrypt(key, sealed)
	if e != nil || !strings.Contains(string(plain), "fixture-password") {
		t.Fatalf("saved credential cannot be reopened: %v", e)
	}
	var status schema.InstallationStatus
	if r := s.JSON(t, "GET", "/api/v1/installation", nil, &status); r.StatusCode != 200 || !status.Configured || !status.Private {
		t.Fatalf("setup state %+v status %d", status, r.StatusCode)
	}
}
func TestUnfinishedSetupIsNotReady(t *testing.T) {
	s := setup(t, nil)
	_, token := account(t, s, true)
	t.Setenv("ZING_REQUIRE_SETUP", "true")
	t.Setenv("ZING_SETUP_COMPLETE", "false")
	if r := s.JSON(t, "GET", "/readyz", nil, nil); r.StatusCode != 503 {
		t.Fatalf("unconfigured instance reported ready %d", r.StatusCode)
	}
	if r := s.JSON(t, "GET", "/api/v1/connections", nil, nil, lidzatest.Bearer(token)); r.StatusCode != 503 {
		t.Fatalf("application bypassed setup %d", r.StatusCode)
	}
	if r := s.JSON(t, "GET", "/api/v1/installation", nil, nil); r.StatusCode != 200 {
		t.Fatalf("setup status unavailable %d", r.StatusCode)
	}
}
