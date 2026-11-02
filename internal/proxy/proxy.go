// Package proxy provides validation and masking for repository proxy URLs
// and helpers to inject proxy environment into subprocesses.
package proxy

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// Validate accepts only http, https and socks5 schemes with a non-empty host.
func Validate(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return err
	}
	switch u.Scheme {
	case "http", "https", "socks5":
	default:
		return fmt.Errorf("unsupported proxy scheme %q (want http, https or socks5)", u.Scheme)
	}
	if u.Host == "" {
		return fmt.Errorf("proxy host is empty")
	}
	return nil
}

// userinfoRe matches scheme://user:password@ inside arbitrary text. The
// password is matched greedily up to the last '@' of the authority, so
// passwords containing '@' are covered. The scheme is matched case-insensitively.
var userinfoRe = regexp.MustCompile(`(?i)([a-z][a-z0-9+.\-]*://[^\s/?#:@]+):[^\s/?#]*@`)

// Mask replaces the password in a URL with ***. URLs without a password are
// returned unchanged.
func Mask(raw string) string {
	if u, err := url.Parse(raw); err == nil {
		if u.User == nil {
			return raw
		}
		if _, has := u.User.Password(); !has {
			return raw
		}
	}
	return MaskText(raw)
}

// MaskText replaces the password of every scheme://user:password@host URL
// found in arbitrary text (error messages, logs) with ***.
func MaskText(text string) string {
	return userinfoRe.ReplaceAllString(text, "${1}:***@")
}

// Scrub replaces every non-empty secret in text with ***.
func Scrub(text string, secrets ...string) string {
	for _, s := range secrets {
		if s != "" {
			text = strings.ReplaceAll(text, s, "***")
		}
	}
	return text
}

// Env returns a copy of base with HTTP_PROXY/HTTPS_PROXY/ALL_PROXY set to
// proxyURL (both upper and lower case variants) and NO_PROXY removed.
// An empty proxyURL returns base unchanged.
func Env(base []string, proxyURL string) []string {
	if proxyURL == "" {
		return base
	}
	out := make([]string, 0, len(base)+3)
	for _, e := range base {
		name := e
		if i := strings.IndexByte(e, '='); i >= 0 {
			name = e[:i]
		}
		switch strings.ToLower(name) {
		case "http_proxy", "https_proxy", "all_proxy", "no_proxy":
			continue
		}
		out = append(out, e)
	}
	for _, name := range []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY"} {
		out = append(out, name+"="+proxyURL)
	}
	return out
}
