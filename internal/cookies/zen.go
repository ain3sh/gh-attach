package cookies

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/browserutils/kooky"
	firefoxbrowser "github.com/browserutils/kooky/browser/firefox"
	"github.com/pierrec/lz4/v4"
	"gopkg.in/ini.v1"
)

var (
	mozLz4Magic = [8]byte{'m', 'o', 'z', 'L', 'z', '4', '0', 0}
	zenRoots    = []string{
		".zen",
		filepath.Join(".var", "app", "app.zen_browser.zen", ".zen"),
		filepath.Join(".var", "app", "app.zen_browser.zen", "zen"),
	}
	firefoxSessionStoreFiles = []string{
		filepath.Join("sessionstore-backups", "recovery.jsonlz4"),
		filepath.Join("sessionstore-backups", "recovery.baklz4"),
		"sessionstore.jsonlz4",
		filepath.Join("sessionstore-backups", "previous.jsonlz4"),
	}
)

type discoveredFirefoxProfile struct {
	Path             string
	Browser          string
	Name             string
	IsDefaultProfile bool
}

type firefoxSessionStoreData struct {
	Cookies []firefoxSessionStoreCookie `json:"cookies"`
}

type firefoxSessionStoreCookie struct {
	Host             string                         `json:"host"`
	Name             string                         `json:"name"`
	Value            string                         `json:"value"`
	Path             string                         `json:"path"`
	Secure           bool                           `json:"secure"`
	HTTPOnly         bool                           `json:"httponly"`
	SameSite         int                            `json:"sameSite"`
	OriginAttributes firefoxSessionStoreOriginAttrs `json:"originAttributes"`
}

type firefoxSessionStoreOriginAttrs struct {
	PartitionKey string `json:"partitionKey"`
}

func discoverZenSessions(ctx context.Context) ([]StoreReport, []string, []browserSession) {
	roots, err := zenProfileRoots()
	if err != nil {
		return nil, []string{fmt.Sprintf("discover Zen profiles: %v", err)}, nil
	}

	var (
		discoveryErrors []string
		reports         []StoreReport
		sessions        []browserSession
		seenProfiles    = map[string]bool{}
	)

	for _, root := range roots {
		profiles, err := findProfilesInRoot(root, "zen")
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			discoveryErrors = append(discoveryErrors, fmt.Sprintf("discover Zen profiles in %s: %v", root, err))
			continue
		}

		for _, profile := range profiles {
			if seenProfiles[profile.Path] {
				continue
			}
			seenProfiles[profile.Path] = true

			report, session := inspectFirefoxDerivedProfile(ctx, profile)
			reports = append(reports, report)
			if session != nil {
				sessions = append(sessions, *session)
			}
		}
	}

	return reports, discoveryErrors, sessions
}

func zenProfileRoots() ([]string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	seen := map[string]bool{}
	roots := make([]string, 0, len(zenRoots))
	for _, root := range zenRoots {
		path := filepath.Clean(filepath.Join(home, root))
		if seen[path] {
			continue
		}
		seen[path] = true
		roots = append(roots, path)
	}

	return roots, nil
}

func findProfilesInRoot(rootDir, browserName string) ([]discoveredFirefoxProfile, error) {
	profilesINI := filepath.Join(rootDir, "profiles.ini")
	config, err := ini.Load(profilesINI)
	if err != nil {
		return nil, err
	}

	var (
		defaultProfileFolder  string
		fallbackProfileFolder string
		fallbackCount         int
	)

	for _, sectionName := range config.SectionStrings() {
		section := config.Section(sectionName)
		if section.Key("Locked").String() == "1" {
			defaultProfileFolder = section.Key("Default").String()
		}
		if strings.HasPrefix(sectionName, "Profile") && section.Key("Default").String() == "1" {
			fallbackProfileFolder = section.Key("Path").String()
			fallbackCount++
		}
	}

	if defaultProfileFolder == "" && fallbackCount == 1 {
		defaultProfileFolder = fallbackProfileFolder
	}

	var profiles []discoveredFirefoxProfile
	for _, sectionName := range config.SectionStrings() {
		if !strings.HasPrefix(sectionName, "Profile") {
			continue
		}

		section := config.Section(sectionName)
		profilePath := filepath.FromSlash(section.Key("Path").String())
		if section.Key("IsRelative").String() == "1" {
			profilePath = filepath.Join(rootDir, profilePath)
		}

		profiles = append(profiles, discoveredFirefoxProfile{
			Path:             profilePath,
			Browser:          browserName,
			Name:             section.Key("Name").String(),
			IsDefaultProfile: defaultProfileFolder != "" && section.Key("Path").String() == defaultProfileFolder,
		})
	}

	return profiles, nil
}

func inspectFirefoxDerivedProfile(ctx context.Context, profile discoveredFirefoxProfile) (StoreReport, *browserSession) {
	report := StoreReport{
		Browser:        profile.Browser,
		Profile:        profile.Name,
		DefaultProfile: profile.IsDefaultProfile,
		FilePath:       profile.Path,
	}

	cookies, hasGitHubSession, err := readGitHubCookiesFromFirefoxProfile(ctx, profile.Path)
	report.CookieCount = len(cookies)
	report.HasGitHubSession = hasGitHubSession
	if err != nil {
		report.Error = err.Error()
	}
	if !hasGitHubSession {
		return report, nil
	}

	return report, &browserSession{
		Cookies: cookies,
		Source: &Source{
			Kind:           SourceBrowser,
			Browser:        report.Browser,
			Profile:        report.Profile,
			DefaultProfile: report.DefaultProfile,
			FilePath:       report.FilePath,
		},
	}
}

func readGitHubCookiesFromFirefoxProfile(ctx context.Context, profileDir string) ([]*http.Cookie, bool, error) {
	var (
		cookies []*http.Cookie
		errs    []error
	)

	sqliteCookies, err := readGitHubCookiesFromFirefoxSQLite(ctx, filepath.Join(profileDir, "cookies.sqlite"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		errs = append(errs, fmt.Errorf("read cookies.sqlite: %w", err))
	}
	cookies = mergeCookies(cookies, sqliteCookies)

	sessionCookies, err := readGitHubCookiesFromSessionStore(profileDir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		errs = append(errs, fmt.Errorf("read sessionstore cookies: %w", err))
	}
	cookies = mergeCookies(cookies, sessionCookies)

	hasGitHubSession := hasCookieNamed(cookies, "user_session")
	if len(errs) == 0 {
		return cookies, hasGitHubSession, nil
	}
	return cookies, hasGitHubSession, errors.Join(errs...)
}

func readGitHubCookiesFromFirefoxSQLite(ctx context.Context, sqlitePath string) ([]*http.Cookie, error) {
	if _, err := os.Stat(sqlitePath); err != nil {
		return nil, err
	}

	cookies, err := firefoxbrowser.ReadCookies(
		ctx,
		sqlitePath,
		kooky.Valid,
		kooky.DomainHasSuffix("github.com"),
	)
	if err != nil {
		return nil, err
	}

	result := make([]*http.Cookie, 0, len(cookies))
	for _, cookie := range cookies {
		if cookie == nil {
			continue
		}
		httpCookie := cookie.Cookie
		result = append(result, &httpCookie)
	}

	return result, nil
}

func readGitHubCookiesFromSessionStore(profileDir string) ([]*http.Cookie, error) {
	var errs []error

	for _, relativePath := range firefoxSessionStoreFiles {
		path := filepath.Join(profileDir, relativePath)
		cookies, err := readGitHubCookiesFromSessionStoreFile(path)
		if err == nil {
			return cookies, nil
		}
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		errs = append(errs, fmt.Errorf("%s: %w", relativePath, err))
	}

	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}

	return nil, os.ErrNotExist
}

func readGitHubCookiesFromSessionStoreFile(path string) ([]*http.Cookie, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	data, err := decompressMozLz4(file)
	if err != nil {
		return nil, err
	}

	var store firefoxSessionStoreData
	if err := json.Unmarshal(data, &store); err != nil {
		return nil, err
	}

	cookies := make([]*http.Cookie, 0, len(store.Cookies))
	for _, rawCookie := range store.Cookies {
		if !isGitHubCookieDomain(rawCookie.Host) {
			continue
		}

		cookie := &http.Cookie{
			Name:        rawCookie.Name,
			Value:       rawCookie.Value,
			Domain:      rawCookie.Host,
			Path:        rawCookie.Path,
			Secure:      rawCookie.Secure,
			HttpOnly:    rawCookie.HTTPOnly,
			SameSite:    firefoxSameSite(rawCookie.SameSite),
			Partitioned: rawCookie.OriginAttributes.PartitionKey != "",
		}
		cookies = append(cookies, cookie)
	}

	return cookies, nil
}

func decompressMozLz4(reader io.Reader) ([]byte, error) {
	var magic [8]byte
	if _, err := io.ReadFull(reader, magic[:]); err != nil {
		return nil, fmt.Errorf("reading mozlz4 magic: %w", err)
	}
	if magic != mozLz4Magic {
		return nil, errors.New("not a mozlz4 file")
	}

	var size uint32
	if err := binary.Read(reader, binary.LittleEndian, &size); err != nil {
		return nil, fmt.Errorf("reading uncompressed size: %w", err)
	}

	compressed, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("reading compressed data: %w", err)
	}

	out := make([]byte, size)
	n, err := lz4.UncompressBlock(compressed, out)
	if err != nil {
		return nil, fmt.Errorf("lz4 decompress: %w", err)
	}

	return out[:n], nil
}

func firefoxSameSite(value int) http.SameSite {
	switch value {
	case 1:
		return http.SameSiteNoneMode
	case 2:
		return http.SameSiteLaxMode
	case 3:
		return http.SameSiteStrictMode
	default:
		return http.SameSiteDefaultMode
	}
}

func isGitHubCookieDomain(domain string) bool {
	cleanDomain := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(domain)), ".")
	return cleanDomain == "github.com" || strings.HasSuffix(cleanDomain, ".github.com")
}

func mergeCookies(groups ...[]*http.Cookie) []*http.Cookie {
	var (
		merged []*http.Cookie
		index  = map[string]int{}
	)

	for _, group := range groups {
		for _, cookie := range group {
			if cookie == nil {
				continue
			}

			copyCookie := *cookie
			key := cookieKey(&copyCookie)
			if existing, ok := index[key]; ok {
				merged[existing] = &copyCookie
				continue
			}

			index[key] = len(merged)
			merged = append(merged, &copyCookie)
		}
	}

	return merged
}

func cookieKey(cookie *http.Cookie) string {
	if cookie == nil {
		return ""
	}
	return strings.Join([]string{
		strings.ToLower(cookie.Name),
		strings.ToLower(cookie.Domain),
		cookie.Path,
	}, "\x00")
}

func hasCookieNamed(cookies []*http.Cookie, name string) bool {
	for _, cookie := range cookies {
		if cookie != nil && cookie.Name == name {
			return true
		}
	}
	return false
}
