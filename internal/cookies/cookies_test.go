package cookies

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/pierrec/lz4/v4"
)

func TestResolveGitHubAuthFromLegacySessionFile(t *testing.T) {
	t.Parallel()

	sessionFile := filepath.Join(t.TempDir(), "session.txt")
	if err := os.WriteFile(sessionFile, []byte("file-session\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	auth, err := ResolveGitHubAuth(context.Background(), ResolveOptions{SessionFile: sessionFile})
	if err != nil {
		t.Fatalf("ResolveGitHubAuth() error = %v", err)
	}
	if len(auth.Cookies) != 2 {
		t.Fatalf("cookie count = %d, want 2 with synthesized same-site cookie", len(auth.Cookies))
	}
	if auth.Cookies[0].Value != "file-session" {
		t.Fatalf("cookie value = %q, want file-session", auth.Cookies[0].Value)
	}
	if auth.Source == nil || auth.Source.Kind != SourceSessionFile {
		t.Fatalf("source = %+v, want session-file", auth.Source)
	}
}

func TestResolveGitHubAuthFromEnvironment(t *testing.T) {
	t.Setenv(SessionCookieEnvVar, "env-session")

	auth, err := ResolveGitHubAuth(context.Background(), ResolveOptions{})
	if err != nil {
		t.Fatalf("ResolveGitHubAuth() error = %v", err)
	}
	if len(auth.Cookies) != 2 {
		t.Fatalf("cookie count = %d, want 2 with synthesized same-site cookie", len(auth.Cookies))
	}
	if auth.Cookies[0].Value != "env-session" {
		t.Fatalf("cookie value = %q, want env-session", auth.Cookies[0].Value)
	}
	if auth.Source == nil || auth.Source.Kind != SourceEnvironment {
		t.Fatalf("source = %+v, want environment", auth.Source)
	}
}

func TestExportGitHubSessionWritesSecureSessionFile(t *testing.T) {
	t.Setenv(SessionCookieEnvVar, "export-session")
	sessionFile := filepath.Join(t.TempDir(), "nested", "session.txt")

	source, err := ExportGitHubSession(context.Background(), sessionFile)
	if err != nil {
		t.Fatalf("ExportGitHubSession() error = %v", err)
	}
	if source == nil || source.Kind != SourceEnvironment {
		t.Fatalf("source = %+v, want environment", source)
	}

	content, err := os.ReadFile(sessionFile)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	var payload exportedAuth
	if err := json.Unmarshal(content, &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if len(payload.Cookies) != 2 {
		t.Fatalf("exported cookie count = %d, want 2", len(payload.Cookies))
	}
	if payload.Cookies[0].Name != "user_session" || payload.Cookies[0].Value != "export-session" {
		t.Fatalf("first exported cookie = %+v", payload.Cookies[0])
	}

	info, err := os.Stat(sessionFile)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("session file permissions = %o, want 600", info.Mode().Perm())
	}
}

func TestDoctorReportsSelectedSessionFileAndEnvironment(t *testing.T) {
	t.Setenv(SessionCookieEnvVar, "env-session")
	sessionFile := filepath.Join(t.TempDir(), "session.txt")
	if err := writeSessionFile(sessionFile, []*http.Cookie{
		{Name: "user_session", Value: "file-session", Domain: "github.com", Path: "/"},
		{Name: "logged_in", Value: "yes", Domain: "github.com", Path: "/"},
	}); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	report, err := Doctor(context.Background(), ResolveOptions{SessionFile: sessionFile})
	if err != nil {
		t.Fatalf("Doctor() error = %v", err)
	}
	if report.SessionFileStatus != "loaded (2 cookies)" {
		t.Fatalf("SessionFileStatus = %q, want loaded (2 cookies)", report.SessionFileStatus)
	}
	if report.EnvironmentStatus != "set" {
		t.Fatalf("EnvironmentStatus = %q, want set", report.EnvironmentStatus)
	}
	if report.Selected == nil || report.Selected.Kind != SourceSessionFile {
		t.Fatalf("Selected = %+v, want session-file", report.Selected)
	}
}

func TestResolveGitHubAuthFromZenSessionStore(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	zenRoot := filepath.Join(homeDir, ".zen")
	profileDir := filepath.Join(zenRoot, "zen-profile.default")

	if err := os.MkdirAll(profileDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(zenRoot, "profiles.ini"), []byte(`
[Profile0]
Name=Personal
IsRelative=1
Path=zen-profile.default
Default=1
`), 0o644); err != nil {
		t.Fatalf("WriteFile(profiles.ini) error = %v", err)
	}

	writeMozLz4SessionStore(t, filepath.Join(profileDir, "sessionstore-backups", "recovery.jsonlz4"), firefoxSessionStoreData{
		Cookies: []firefoxSessionStoreCookie{
			{
				Host:     "github.com",
				Name:     "user_session",
				Value:    "zen-user-session",
				Path:     "/",
				Secure:   true,
				HTTPOnly: true,
				SameSite: 2,
			},
			{
				Host:     "github.com",
				Name:     "_gh_sess",
				Value:    "zen-gh-sess",
				Path:     "/",
				Secure:   true,
				HTTPOnly: true,
				SameSite: 2,
			},
			{
				Host:     "example.com",
				Name:     "ignored",
				Value:    "nope",
				Path:     "/",
				SameSite: 2,
			},
		},
	})

	auth, err := ResolveGitHubAuth(context.Background(), ResolveOptions{})
	if err != nil {
		t.Fatalf("ResolveGitHubAuth() error = %v", err)
	}
	if auth.Source == nil || auth.Source.Kind != SourceBrowser || auth.Source.Browser != "zen" {
		t.Fatalf("source = %+v, want zen browser source", auth.Source)
	}
	if auth.Source.Profile != "Personal" {
		t.Fatalf("profile = %q, want Personal", auth.Source.Profile)
	}
	if !auth.Source.DefaultProfile {
		t.Fatalf("DefaultProfile = false, want true")
	}
	if !hasCookieNamed(auth.Cookies, "user_session") {
		t.Fatalf("resolved cookies missing user_session")
	}
	if !hasCookieNamed(auth.Cookies, "_gh_sess") {
		t.Fatalf("resolved cookies missing _gh_sess")
	}
	if !hasCookieNamed(auth.Cookies, "__Host-user_session_same_site") {
		t.Fatalf("resolved cookies missing synthesized same-site cookie")
	}
	if hasCookieNamed(auth.Cookies, "ignored") {
		t.Fatalf("resolved cookies should not include non-GitHub cookies")
	}
}

func writeMozLz4SessionStore(t *testing.T, path string, store firefoxSessionStoreData) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	payload, err := json.Marshal(store)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	compressed := make([]byte, lz4.CompressBlockBound(len(payload)))
	n, err := lz4.CompressBlock(payload, compressed, nil)
	if err != nil {
		t.Fatalf("CompressBlock() error = %v", err)
	}
	if n == 0 {
		t.Fatalf("CompressBlock() returned 0 bytes")
	}

	var buffer bytes.Buffer
	buffer.Write(mozLz4Magic[:])
	if err := binary.Write(&buffer, binary.LittleEndian, uint32(len(payload))); err != nil {
		t.Fatalf("binary.Write() error = %v", err)
	}
	buffer.Write(compressed[:n])

	if err := os.WriteFile(path, buffer.Bytes(), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
}
