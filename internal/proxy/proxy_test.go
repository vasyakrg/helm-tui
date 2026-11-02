package proxy

import (
	"strings"
	"testing"
)

func TestValidate(t *testing.T) {
	cases := []struct {
		raw    string
		wantOK bool
	}{
		{"http://h:3128", true},
		{"https://h", true},
		{"socks5://h:1080", true},
		{"http://u:p@h:3128", true},
		{"ftp://h", false},
		{"", false},
		{"http://", false},
		{"h:3128", false},
	}
	for _, c := range cases {
		err := Validate(c.raw)
		if (err == nil) != c.wantOK {
			t.Errorf("Validate(%q) err=%v, wantOK=%v", c.raw, err, c.wantOK)
		}
	}
}

func TestMask_HidesPassword(t *testing.T) {
	got := Mask("http://user:secret@h:3128")
	if strings.Contains(got, "secret") {
		t.Errorf("Mask leaked password: %q", got)
	}
	if !strings.Contains(got, "http://user:***@h:3128") {
		t.Errorf("Mask = %q, want masked form", got)
	}
	if Mask("http://h:3128") != "http://h:3128" {
		t.Errorf("Mask changed URL without credentials")
	}
}

func TestScrub_RemovesProxyPasswordFromText(t *testing.T) {
	text := "dial via http://u:secret@h failed"
	got := Scrub(text, "secret")
	if strings.Contains(got, "secret") {
		t.Errorf("Scrub leaked secret: %q", got)
	}
	if !strings.Contains(got, "***") {
		t.Errorf("Scrub = %q, want ***", got)
	}
}

func TestScrub_EmptySecretsIgnored(t *testing.T) {
	if got := Scrub("text", ""); got != "text" {
		t.Errorf("Scrub with empty secret = %q", got)
	}
}

func TestEnv_OverridesAndClearsNoProxy(t *testing.T) {
	base := []string{
		"HTTP_PROXY=old",
		"http_proxy=old",
		"NO_PROXY=x",
		"no_proxy=y",
		"PATH=/usr/bin",
		"HOME=/home/u",
	}
	got := Env(base, "http://new:3128")

	count := func(name string) int {
		n := 0
		for _, e := range got {
			if strings.HasPrefix(strings.ToUpper(e), name+"=") {
				n++
			}
		}
		return n
	}
	for _, name := range []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY"} {
		if n := count(name); n != 1 {
			t.Errorf("got %d entries for %s, want 1", n, name)
		}
		if !strings.Contains(getValue(got, name), "http://new:3128") {
			t.Errorf("%s = %q, want proxy value", name, getValue(got, name))
		}
	}
	for _, e := range got {
		name := strings.SplitN(e, "=", 2)[0]
		if strings.EqualFold(name, "NO_PROXY") {
			t.Errorf("NO_PROXY not cleared: %q", e)
		}
	}
	if getValue(got, "PATH") != "/usr/bin" || getValue(got, "HOME") != "/home/u" {
		t.Errorf("other vars not preserved: %v", got)
	}
}

func getValue(env []string, name string) string {
	for _, e := range env {
		if strings.EqualFold(strings.SplitN(e, "=", 2)[0], name) {
			return strings.SplitN(e, "=", 2)[1]
		}
	}
	return ""
}

func TestEnv_EmptyProxyKeepsBase(t *testing.T) {
	base := []string{"A=1", "HTTP_PROXY=x"}
	got := Env(base, "")
	if len(got) != len(base) {
		t.Fatalf("Env changed base length: %v", got)
	}
	for i := range base {
		if got[i] != base[i] {
			t.Errorf("Env modified base[%d]: %q != %q", i, got[i], base[i])
		}
	}
}

func TestMask_CaseInsensitiveSchemeAndAtInPassword(t *testing.T) {
	cases := map[string]string{
		"HTTP://u:secret@h:3128":  "HTTP://u:***@h:3128",
		"http://u:p@ss@h:3128/x":  "http://u:***@h:3128/x",
		"socks5://u:pa:ss@h:1080": "socks5://u:***@h:1080",
		"https://user@h:3128":     "https://user@h:3128",
		"http://h:3128/a@b":       "http://h:3128/a@b",
	}
	for in, want := range cases {
		if got := Mask(in); got != want {
			t.Errorf("Mask(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMaskText_AllURLsAndAtInPassword(t *testing.T) {
	in := `Get "HTTPS://a:p@ss1@h1/index.yaml": dial via http://b:s3cret@h2:3128 failed; mail x@y.z, http://h3:80 ok`
	got := MaskText(in)
	for _, leak := range []string{"p@ss1", "s3cret"} {
		if strings.Contains(got, leak) {
			t.Errorf("MaskText leaked %q: %q", leak, got)
		}
	}
	for _, keep := range []string{"HTTPS://a:***@h1/index.yaml", "http://b:***@h2:3128", "x@y.z", "http://h3:80 ok"} {
		if !strings.Contains(got, keep) {
			t.Errorf("MaskText lost %q: %q", keep, got)
		}
	}
}
