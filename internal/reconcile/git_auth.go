package reconcile

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/go-git/go-git/v5/plumbing/transport"
	gitclient "github.com/go-git/go-git/v5/plumbing/transport/client"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
)

const redactedGitURL = "[redacted invalid repository URL]"

var repositoryUserinfoPattern = regexp.MustCompile(`(?i)([a-z][a-z0-9+.-]*://)[^/@\s]+@`)

func init() {
	// go-git selects HTTP transports from a process-global protocol registry.
	// Install the redirect policy once, before any reconciliation goroutines
	// start, so clone and fetch share the same credential boundary.
	gitclient.InstallProtocol("https", githttp.NewClient(newGitHTTPClient(http.DefaultTransport)))
}

// ResolveGitAuth returns the authentication method for repoURL. HTTPS
// credentials are read only from their BOSUN_ environment variables and are
// kept operation-scoped; SSH resolution retains its existing behavior.
func ResolveGitAuth(repoURL string) (transport.AuthMethod, error) {
	if err := rejectRepositoryUserinfo(repoURL); err != nil {
		return nil, err
	}

	username := os.Getenv("BOSUN_GIT_USERNAME")
	token := os.Getenv("BOSUN_GIT_TOKEN")
	if username == "" && token == "" {
		return getSSHAuth(repoURL)
	}
	if username == "" {
		return nil, errors.New("BOSUN_GIT_USERNAME is required when BOSUN_GIT_TOKEN is set")
	}
	if token == "" {
		return nil, errors.New("BOSUN_GIT_TOKEN is required when BOSUN_GIT_USERNAME is set")
	}

	parsed, err := url.Parse(repoURL)
	if err != nil || !parsed.IsAbs() || !strings.EqualFold(parsed.Scheme, "https") || parsed.Host == "" {
		return nil, errors.New("BOSUN_GIT_USERNAME and BOSUN_GIT_TOKEN require an absolute https:// repository URL with a host")
	}

	return &githttp.BasicAuth{Username: username, Password: token}, nil
}

// ValidateGitAuthentication applies the same pre-network contract used by
// clone and fetch without retaining the returned authentication value.
func ValidateGitAuthentication(repoURL string) error {
	_, err := ResolveGitAuth(repoURL)
	return err
}

func rejectRepositoryUserinfo(repoURL string) error {
	parsed, err := url.Parse(repoURL)
	if err != nil {
		// A malformed standard URL must not flow to go-git, whose error may echo
		// the unsafe raw value. SCP-like SSH syntax parses as a relative path and
		// reaches the existing SSH resolver below.
		if strings.Contains(repoURL, "://") {
			return errors.New("repository URL is malformed; use a URL without embedded credentials")
		}
		return nil
	}
	if parsed.User != nil {
		return errors.New("repository URL userinfo is not allowed; use BOSUN_GIT_USERNAME and BOSUN_GIT_TOKEN")
	}
	return nil
}

func newGitHTTPClient(transport http.RoundTripper) *http.Client {
	if transport == nil {
		transport = http.DefaultTransport
	}
	return &http.Client{
		Transport:     transport,
		CheckRedirect: checkGitRedirect,
	}
}

func checkGitRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= 10 {
		return errors.New("stopped after 10 redirects")
	}
	if len(via) == 0 {
		return nil
	}

	authorization := via[0].Header.Get("Authorization")
	if authorization == "" {
		return nil
	}

	origin := via[0].URL
	if req.URL.User != nil || !strings.EqualFold(req.URL.Scheme, "https") || !sameHTTPSOrigin(origin, req.URL) {
		req.Header.Del("Authorization")
		return errors.New("authenticated Git redirect rejected: destination must remain on the configured HTTPS origin")
	}

	// net/http normally retains sensitive headers for a same-origin redirect.
	// Set it explicitly so the approved redirect behavior does not depend on
	// that implementation detail.
	req.Header.Set("Authorization", authorization)
	return nil
}

func sameHTTPSOrigin(a, b *url.URL) bool {
	return strings.EqualFold(a.Scheme, "https") &&
		strings.EqualFold(b.Scheme, "https") &&
		strings.EqualFold(a.Hostname(), b.Hostname()) &&
		effectiveHTTPSPort(a) == effectiveHTTPSPort(b)
}

func effectiveHTTPSPort(u *url.URL) string {
	if port := u.Port(); port != "" {
		return port
	}
	return "443"
}

// SanitizeGitURL removes URL userinfo before presentation. Raw malformed
// standard URLs are replaced rather than echoed.
func SanitizeGitURL(repoURL string) string {
	parsed, err := url.Parse(repoURL)
	if err != nil {
		return redactedGitURL
	}
	if parsed.User != nil {
		parsed.User = nil
	}
	return parsed.String()
}

// SanitizeGitText removes configured credentials and their common encodings
// from transport errors and daemon response fields.
func SanitizeGitText(value string) string {
	username := os.Getenv("BOSUN_GIT_USERNAME")
	token := os.Getenv("BOSUN_GIT_TOKEN")

	redacted := repositoryUserinfoPattern.ReplaceAllString(value, `${1}`)
	variantSet := make(map[string]struct{})
	addVariant := func(variant string) {
		if variant != "" {
			variantSet[variant] = struct{}{}
		}
	}
	for _, secret := range []string{username, token} {
		if secret == "" {
			continue
		}
		for _, variant := range []string{
			secret, url.QueryEscape(secret), url.PathEscape(secret),
			strings.ToLower(url.QueryEscape(secret)),
			strings.ToLower(url.PathEscape(secret)),
		} {
			addVariant(variant)
		}
	}
	if username != "" && token != "" {
		userinfo := url.UserPassword(username, token).String()
		addVariant(userinfo)
		addVariant(strings.ToLower(userinfo))
		encoded := base64.StdEncoding.EncodeToString([]byte(username + ":" + token))
		addVariant(encoded)
		addVariant("Basic " + encoded)
	}

	// Replace all variants in one longest-first pass. Sequential ReplaceAll
	// calls can partially expose an overlapping secret (for example username
	// "a" and token "abc") and then rescan the replacement marker itself.
	variants := make([]string, 0, len(variantSet))
	for variant := range variantSet {
		variants = append(variants, variant)
	}
	sort.Slice(variants, func(i, j int) bool {
		if len(variants[i]) == len(variants[j]) {
			return variants[i] < variants[j]
		}
		return len(variants[i]) > len(variants[j])
	})
	if len(variants) > 0 {
		pairs := make([]string, 0, len(variants)*2)
		for _, variant := range variants {
			pairs = append(pairs, variant, "[redacted]")
		}
		redacted = strings.NewReplacer(pairs...).Replace(redacted)
	}
	return redacted
}

// SanitizeGitError returns a presentation-safe copy of err.
func SanitizeGitError(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s", SanitizeGitText(err.Error()))
}
