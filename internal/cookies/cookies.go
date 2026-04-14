package cookies

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/browserutils/kooky"
	_ "github.com/browserutils/kooky/browser/brave"
	_ "github.com/browserutils/kooky/browser/chrome"
	_ "github.com/browserutils/kooky/browser/chromium"
	_ "github.com/browserutils/kooky/browser/edge"
	_ "github.com/browserutils/kooky/browser/firefox"
)

const SessionCookieEnvVar = "GH_ATTACH_USER_SESSION"

type SourceKind string

const (
	SourceSessionFile SourceKind = "session-file"
	SourceEnvironment SourceKind = "environment"
	SourceBrowser     SourceKind = "browser"
)

type ResolveOptions struct {
	SessionFile string
}

type Auth struct {
	Cookies []*http.Cookie
	Source  *Source
}

type Source struct {
	Kind           SourceKind
	SessionFile    string
	Browser        string
	Profile        string
	DefaultProfile bool
	FilePath       string
}

type StoreReport struct {
	Browser          string
	Profile          string
	DefaultProfile   bool
	FilePath         string
	HasGitHubSession bool
	CookieCount      int
	Error            string
}

type DoctorReport struct {
	SessionFile       string
	SessionFileStatus string
	EnvironmentStatus string
	Stores            []StoreReport
	DiscoveryErrors   []string
	Selected          *Source
}

type exportedAuth struct {
	Version int              `json:"version"`
	Cookies []exportedCookie `json:"cookies"`
}

type exportedCookie struct {
	Name        string        `json:"name"`
	Value       string        `json:"value"`
	Path        string        `json:"path,omitempty"`
	Domain      string        `json:"domain,omitempty"`
	Expires     string        `json:"expires,omitempty"`
	RawExpires  string        `json:"raw_expires,omitempty"`
	MaxAge      int           `json:"max_age,omitempty"`
	Secure      bool          `json:"secure,omitempty"`
	HttpOnly    bool          `json:"http_only,omitempty"`
	SameSite    http.SameSite `json:"same_site,omitempty"`
	Partitioned bool          `json:"partitioned,omitempty"`
}

func GetGitHubAuth(opts ResolveOptions) (*Auth, error) {
	return ResolveGitHubAuth(context.Background(), opts)
}

func ResolveGitHubAuth(ctx context.Context, opts ResolveOptions) (*Auth, error) {
	sessionFile := strings.TrimSpace(opts.SessionFile)
	if sessionFile != "" {
		cookies, err := loadSessionFile(sessionFile)
		if err != nil {
			return nil, fmt.Errorf("read --session-file %s: %w", sessionFile, err)
		}
		return &Auth{
			Cookies: ensureSameSiteCookie(cookies),
			Source:  &Source{Kind: SourceSessionFile, SessionFile: sessionFile},
		}, nil
	}

	if value := strings.TrimSpace(os.Getenv(SessionCookieEnvVar)); value != "" {
		return &Auth{
			Cookies: ensureSameSiteCookie([]*http.Cookie{newSessionCookie(value)}),
			Source:  &Source{Kind: SourceEnvironment},
		}, nil
	}

	_, _, sessions := discoverBrowserSessions(ctx)
	if len(sessions) > 0 {
		return &Auth{
			Cookies: ensureSameSiteCookie(sessions[0].Cookies),
			Source:  sessions[0].Source,
		}, nil
	}

	return nil, missingSessionError()
}

func Doctor(ctx context.Context, opts ResolveOptions) (*DoctorReport, error) {
	report := &DoctorReport{
		SessionFile:       strings.TrimSpace(opts.SessionFile),
		SessionFileStatus: "not provided",
		EnvironmentStatus: "not set",
	}

	if report.SessionFile != "" {
		if cookies, err := loadSessionFile(report.SessionFile); err != nil {
			report.SessionFileStatus = err.Error()
		} else {
			report.SessionFileStatus = fmt.Sprintf("loaded (%d cookies)", len(cookies))
		}
	}

	if strings.TrimSpace(os.Getenv(SessionCookieEnvVar)) != "" {
		report.EnvironmentStatus = "set"
	}

	stores, discoveryErrors, _ := discoverBrowserSessions(ctx)
	report.Stores = stores
	report.DiscoveryErrors = discoveryErrors

	auth, err := ResolveGitHubAuth(ctx, opts)
	if auth != nil {
		report.Selected = auth.Source
		return report, err
	}

	return report, err
}

func ExportGitHubSession(ctx context.Context, sessionFile string) (*Source, error) {
	target := strings.TrimSpace(sessionFile)
	if target == "" {
		return nil, fmt.Errorf("--session-file is required")
	}

	auth, err := ResolveGitHubAuth(ctx, ResolveOptions{})
	if err != nil {
		return nil, err
	}

	if err := writeSessionFile(target, auth.Cookies); err != nil {
		return nil, err
	}

	return auth.Source, nil
}

func (s *Source) Describe() string {
	if s == nil {
		return "none"
	}

	switch s.Kind {
	case SourceSessionFile:
		return fmt.Sprintf("session file (%s)", s.SessionFile)
	case SourceEnvironment:
		return fmt.Sprintf("environment variable (%s)", SessionCookieEnvVar)
	case SourceBrowser:
		defaultLabel := ""
		if s.DefaultProfile {
			defaultLabel = ", default profile"
		}
		if s.Profile != "" {
			return fmt.Sprintf("browser store (%s / %s%s)", s.Browser, s.Profile, defaultLabel)
		}
		return fmt.Sprintf("browser store (%s%s)", s.Browser, defaultLabel)
	default:
		return string(s.Kind)
	}
}

type browserSession struct {
	Cookies []*http.Cookie
	Source  *Source
}

func discoverBrowserSessions(ctx context.Context) ([]StoreReport, []string, []browserSession) {
	var (
		discoveryErrors []string
		reports         []StoreReport
		sessions        []browserSession
	)

	for store, err := range kooky.TraverseCookieStores(ctx) {
		if err != nil {
			continue
		}
		if store == nil {
			continue
		}

		report := StoreReport{
			Browser:        store.Browser(),
			Profile:        store.Profile(),
			DefaultProfile: store.IsDefaultProfile(),
			FilePath:       store.FilePath(),
		}

		storeCookies, hasGitHubSession, readErr := readGitHubCookiesFromStore(ctx, store)
		if readErr != nil {
			report.Error = readErr.Error()
		}
		report.CookieCount = len(storeCookies)
		report.HasGitHubSession = hasGitHubSession
		if hasGitHubSession {
			sessions = append(sessions, browserSession{
				Cookies: storeCookies,
				Source: &Source{
					Kind:           SourceBrowser,
					Browser:        report.Browser,
					Profile:        report.Profile,
					DefaultProfile: report.DefaultProfile,
					FilePath:       report.FilePath,
				},
			})
		}

		reports = append(reports, report)
		_ = store.Close()
	}

	zenReports, zenErrors, zenSessions := discoverZenSessions(ctx)
	reports = append(reports, zenReports...)
	discoveryErrors = append(discoveryErrors, zenErrors...)
	sessions = append(sessions, zenSessions...)

	sort.SliceStable(reports, func(i, j int) bool {
		return compareStoreReports(reports[i], reports[j])
	})
	sort.SliceStable(sessions, func(i, j int) bool {
		return compareStoreReports(
			StoreReport{
				Browser:        sessions[i].Source.Browser,
				Profile:        sessions[i].Source.Profile,
				DefaultProfile: sessions[i].Source.DefaultProfile,
				FilePath:       sessions[i].Source.FilePath,
			},
			StoreReport{
				Browser:        sessions[j].Source.Browser,
				Profile:        sessions[j].Source.Profile,
				DefaultProfile: sessions[j].Source.DefaultProfile,
				FilePath:       sessions[j].Source.FilePath,
			},
		)
	})

	return reports, discoveryErrors, sessions
}

func readGitHubCookiesFromStore(ctx context.Context, store kooky.CookieStore) ([]*http.Cookie, bool, error) {
	var firstErr error
	var cookies []*http.Cookie
	hasGitHubSession := false

	for cookie, err := range store.TraverseCookies(
		kooky.Valid,
		kooky.DomainHasSuffix("github.com"),
	) {
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if cookie != nil {
			httpCookie := cookie.Cookie
			cookies = append(cookies, &httpCookie)
			if cookie.Name == "user_session" {
				hasGitHubSession = true
			}
		}

		select {
		case <-ctx.Done():
			return nil, false, ctx.Err()
		default:
		}
	}

	return cookies, hasGitHubSession, firstErr
}

func compareStoreReports(left, right StoreReport) bool {
	if left.DefaultProfile != right.DefaultProfile {
		return left.DefaultProfile
	}

	leftPriority := browserPriority(left.Browser)
	rightPriority := browserPriority(right.Browser)
	if leftPriority != rightPriority {
		return leftPriority < rightPriority
	}

	if left.Browser != right.Browser {
		return left.Browser < right.Browser
	}
	if left.Profile != right.Profile {
		return left.Profile < right.Profile
	}

	return left.FilePath < right.FilePath
}

func browserPriority(browser string) int {
	switch strings.ToLower(browser) {
	case "chrome":
		return 0
	case "brave":
		return 1
	case "chromium":
		return 2
	case "edge":
		return 3
	case "zen":
		return 4
	case "firefox":
		return 5
	default:
		return 99
	}
}

func loadSessionFile(path string) ([]*http.Cookie, error) {
	content, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, err
	}

	value := strings.TrimSpace(string(content))
	if value == "" {
		return nil, fmt.Errorf("session file is empty")
	}

	var exported exportedAuth
	if err := json.Unmarshal([]byte(value), &exported); err == nil && len(exported.Cookies) > 0 {
		return decodeExportedCookies(exported.Cookies)
	}

	return []*http.Cookie{newSessionCookie(value)}, nil
}

func writeSessionFile(path string, cookies []*http.Cookie) error {
	cleanPath := filepath.Clean(path)
	dir := filepath.Dir(cleanPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create session-file directory: %w", err)
	}

	tempFile, err := os.CreateTemp(dir, ".gh-attach-session-*")
	if err != nil {
		return fmt.Errorf("create temporary session file: %w", err)
	}
	tempPath := tempFile.Name()
	defer os.Remove(tempPath)

	if err := tempFile.Chmod(0o600); err != nil {
		tempFile.Close()
		return fmt.Errorf("set session file permissions: %w", err)
	}

	payload, err := json.MarshalIndent(exportedAuth{
		Version: 1,
		Cookies: encodeExportedCookies(cookies),
	}, "", "  ")
	if err != nil {
		tempFile.Close()
		return fmt.Errorf("marshal session file: %w", err)
	}

	if _, err := tempFile.Write(payload); err != nil {
		tempFile.Close()
		return fmt.Errorf("write session file: %w", err)
	}
	if _, err := tempFile.WriteString("\n"); err != nil {
		tempFile.Close()
		return fmt.Errorf("write session file trailing newline: %w", err)
	}

	if err := tempFile.Close(); err != nil {
		return fmt.Errorf("close session file: %w", err)
	}

	if err := os.Rename(tempPath, cleanPath); err != nil {
		return fmt.Errorf("move session file into place: %w", err)
	}

	return nil
}

func encodeExportedCookies(cookies []*http.Cookie) []exportedCookie {
	exported := make([]exportedCookie, 0, len(cookies))
	for _, cookie := range cookies {
		if cookie == nil {
			continue
		}

		record := exportedCookie{
			Name:        cookie.Name,
			Value:       cookie.Value,
			Path:        cookie.Path,
			Domain:      cookie.Domain,
			RawExpires:  cookie.RawExpires,
			MaxAge:      cookie.MaxAge,
			Secure:      cookie.Secure,
			HttpOnly:    cookie.HttpOnly,
			SameSite:    cookie.SameSite,
			Partitioned: cookie.Partitioned,
		}
		if !cookie.Expires.IsZero() {
			record.Expires = cookie.Expires.Format(time.RFC3339Nano)
		}
		exported = append(exported, record)
	}
	return exported
}

func decodeExportedCookies(records []exportedCookie) ([]*http.Cookie, error) {
	cookies := make([]*http.Cookie, 0, len(records))
	for _, record := range records {
		if strings.TrimSpace(record.Name) == "" {
			continue
		}

		cookie := &http.Cookie{
			Name:        record.Name,
			Value:       record.Value,
			Path:        record.Path,
			Domain:      record.Domain,
			RawExpires:  record.RawExpires,
			MaxAge:      record.MaxAge,
			Secure:      record.Secure,
			HttpOnly:    record.HttpOnly,
			SameSite:    record.SameSite,
			Partitioned: record.Partitioned,
		}
		if record.Expires != "" {
			expires, err := time.Parse(time.RFC3339Nano, record.Expires)
			if err != nil {
				return nil, fmt.Errorf("parse cookie expiry for %s: %w", record.Name, err)
			}
			cookie.Expires = expires
		}
		cookies = append(cookies, cookie)
	}

	if len(cookies) == 0 {
		return nil, fmt.Errorf("session file contained no cookies")
	}

	return cookies, nil
}

func ensureSameSiteCookie(cookies []*http.Cookie) []*http.Cookie {
	if len(cookies) == 0 {
		return cookies
	}

	var (
		hasSameSite bool
		userSession *http.Cookie
	)

	result := make([]*http.Cookie, 0, len(cookies)+1)
	for _, cookie := range cookies {
		if cookie == nil {
			continue
		}

		copyCookie := *cookie
		result = append(result, &copyCookie)

		switch copyCookie.Name {
		case "__Host-user_session_same_site":
			hasSameSite = true
		case "user_session":
			userSession = &copyCookie
		}
	}

	if hasSameSite || userSession == nil {
		return result
	}

	result = append(result, &http.Cookie{
		Name:     "__Host-user_session_same_site",
		Value:    userSession.Value,
		Path:     "/",
		Secure:   true,
		HttpOnly: true,
	})

	return result
}

func newSessionCookie(value string) *http.Cookie {
	return &http.Cookie{
		Name:     "user_session",
		Value:    value,
		Domain:   "github.com",
		Path:     "/",
		Secure:   true,
		HttpOnly: true,
	}
}

func missingSessionError() error {
	return fmt.Errorf(
		"no github.com auth cookies found in supported browsers; sign into GitHub in Chrome, Brave, Chromium, Edge, Firefox, or Zen, or use --session-file or set %s",
		SessionCookieEnvVar,
	)
}
