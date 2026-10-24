//go:build desktop

package desktop

import (
	"strings"
	"testing"
)

func TestValidateLoginURL(t *testing.T) {
	for _, rawURL := range []string{
		"https://auth.openai.com/oauth/authorize?state=test-only&code_challenge=fixture",
	} {
		if err := validateLoginURL(rawURL); err != nil {
			t.Fatalf("official HTTPS URL rejected: %v", err)
		}
	}
	for _, rawURL := range []string{
		"http://auth.openai.com/?state=test-only",
		"https://auth.openai.com.attacker.invalid/?state=test-only",
		"https://auth.openai.com@attacker.invalid/?state=test-only",
		"https://user@auth.openai.com/?state=test-only",
		"file:///C:/Windows/System32/cmd.exe?state=test-only",
		"javascript:alert('test-only')",
		"https://auth.openai.com:1234/?state=test-only",
		"%test-only",
	} {
		err := validateLoginURL(rawURL)
		if err == nil {
			t.Error("non-official URL accepted")
		} else if strings.Contains(err.Error(), "test-only") {
			t.Error("authorization parameters leaked into error")
		}
	}
}

func TestValidateProjectURL(t *testing.T) {
	for _, rawURL := range []string{
		"https://github.com/kunghim/CoBot",
		"https://github.com/kunghim/CoBot/releases/tag/v0.1.0-beta.2",
		"https://github.com/kunghim/CoBot/issues/new",
	} {
		if err := validateProjectURL(rawURL); err != nil {
			t.Errorf("project page %q rejected: %v", rawURL, err)
		}
	}
	for _, rawURL := range []string{
		"http://github.com/kunghim/CoBot",
		"https://github.com/kunghim/CoBot-evil/releases",
		"https://github.com/kunghim/Other",
		"https://github.com.attacker.invalid/kunghim/CoBot",
		"https://user@github.com/kunghim/CoBot",
		"https://github.com:444/kunghim/CoBot",
		"file:///C:/Windows/System32/cmd.exe",
	} {
		if err := validateProjectURL(rawURL); err == nil {
			t.Errorf("foreign address %q accepted", rawURL)
		}
	}
}

func TestOpenLoginURLRequiresApplicationContext(t *testing.T) {
	bridge := &Bridge{}
	if err := bridge.OpenLoginURL("https://auth.openai.com/oauth/authorize"); err == nil {
		t.Fatal("expected missing application context error")
	}
}
