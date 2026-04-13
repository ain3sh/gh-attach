package cookies

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/browserutils/kooky"
	_ "github.com/browserutils/kooky/browser/brave"
	_ "github.com/browserutils/kooky/browser/chrome"
	_ "github.com/browserutils/kooky/browser/chromium"
	_ "github.com/browserutils/kooky/browser/edge"
)

const sessionCookieEnvVar = "GH_ATTACH_USER_SESSION"

func GetGitHubSession() (*http.Cookie, error) {
	if value := strings.TrimSpace(os.Getenv(sessionCookieEnvVar)); value != "" {
		return &http.Cookie{
			Name:     "user_session",
			Value:    value,
			Domain:   "github.com",
			Path:     "/",
			Secure:   true,
			HttpOnly: true,
		}, nil
	}

	ctx := context.Background()
	cookies, err := kooky.ReadCookies(
		ctx,
		kooky.Valid,
		kooky.DomainHasSuffix("github.com"),
		kooky.Name("user_session"),
	)

	if len(cookies) > 0 {
		return &cookies[0].Cookie, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading browser cookies: %w", err)
	}

	return nil, fmt.Errorf("no github.com user_session cookie found in any supported browser; either sign into GitHub in a supported browser or set %s", sessionCookieEnvVar)
}
