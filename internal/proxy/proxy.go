// Package proxy provides validation and masking for repository proxy URLs
// and helpers to inject proxy environment into subprocesses.
package proxy

import (
	"fmt"
	"net/url"
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

// Mask replaces the password in a proxy URL with ***.
func Mask(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.User == nil {
		return raw
	}
	if _, has := u.User.Password(); !has {
		return raw
	}
	// url.UserPassword percent-encodes ***, so splice userinfo by hand.
	scheme := u.Scheme + "://"
	rest := strings.TrimPrefix(raw, scheme)
	slash := strings.Index(rest, "/")
	hostPart := rest
	path := ""
	if slash >= 0 {
		hostPart, path = rest[:slash], rest[slash:]
	}
	at := strings.LastIndex(hostPart, "@")
	if at < 0 {
		return raw
	}
	user := hostPart[:at]
	if i := strings.IndexByte(user, ':'); i >= 0 {
		user = user[:i]
	}
	return scheme + user + ":***@" + hostPart[at+1:] + path
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
