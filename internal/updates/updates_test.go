package updates

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func listing(w http.ResponseWriter, body string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body))
}

func check(t *testing.T, current, body string, status int) Result {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { listing(w, body, status) }))
	defer server.Close()
	return New(current, server.URL).Check(context.Background())
}

const releases = `[
 {"tag_name":"v0.1.0-beta.2","html_url":"https://github.com/kunghim/CoBot/releases/tag/v0.1.0-beta.2","published_at":"2026-10-20T00:00:00Z"},
 {"tag_name":"v0.1.0-beta.10","html_url":"https://github.com/kunghim/CoBot/releases/tag/v0.1.0-beta.10","published_at":"2026-10-23T00:00:00Z"},
 {"tag_name":"v0.0.9","html_url":"https://github.com/kunghim/CoBot/releases/tag/v0.0.9"},
 {"tag_name":"v9.9.9","draft":true,"html_url":"https://github.com/kunghim/CoBot/releases/tag/v9.9.9"},
 {"tag_name":"v8.8.8","html_url":"https://evil.example/releases/tag/v8.8.8"}
]`

func TestNewerReleaseIsAvailableAndPreReleasePartsCompareAsNumbers(t *testing.T) {
	got := check(t, "0.1.0-beta.2", releases, 200)
	if got.State != Available || got.Latest != "0.1.0-beta.10" {
		t.Fatalf("got %+v", got)
	}
	if got.ReleaseURL != "https://github.com/kunghim/CoBot/releases/tag/v0.1.0-beta.10" {
		t.Fatalf("release url %q", got.ReleaseURL)
	}
}

func TestDraftsAndForeignPagesAreNeverOffered(t *testing.T) {
	got := check(t, "0.1.0-beta.10", releases, 200)
	if got.State != UpToDate || got.Latest != "0.1.0-beta.10" {
		t.Fatalf("got %+v", got)
	}
}

func TestReleaseOutranksItsOwnPreReleases(t *testing.T) {
	body := `[{"tag_name":"v0.1.0","html_url":"https://github.com/kunghim/CoBot/releases/tag/v0.1.0"}]`
	if got := check(t, "0.1.0-beta.9", body, 200); got.State != Available {
		t.Fatalf("beta against release: %+v", got)
	}
	if got := check(t, "0.1.0", body, 200); got.State != UpToDate {
		t.Fatalf("same release: %+v", got)
	}
	if got := check(t, "0.2.0", body, 200); got.State != UpToDate {
		t.Fatalf("installed is newer: %+v", got)
	}
}

func TestADevelopmentBuildCannotBeCompared(t *testing.T) {
	got := check(t, "dev", releases, 200)
	if got.State != Unknown || got.Latest == "" {
		t.Fatalf("got %+v", got)
	}
}

func TestALocalBuildStampIsNotAReleaseNumber(t *testing.T) {
	for _, current := range []string{"0.1.0-dev", "0.1.0-beta.2-dev"} {
		if got := check(t, current, releases, 200); got.State != Unknown {
			t.Fatalf("%s: %+v", current, got)
		}
	}
}

func TestFailuresAreStatesNotErrors(t *testing.T) {
	if got := check(t, "0.1.0", `{"message":"rate limited"}`, 403); got.State != Rejected {
		t.Fatalf("403: %+v", got)
	}
	if got := check(t, "0.1.0", `not json`, 200); got.State != Rejected {
		t.Fatalf("bad body: %+v", got)
	}
	if got := check(t, "0.1.0", `[]`, 200); got.State != Rejected {
		t.Fatalf("no releases: %+v", got)
	}
	server := httptest.NewServer(http.NotFoundHandler())
	url := server.URL
	server.Close()
	if got := New("0.1.0", url).Check(context.Background()); got.State != Unreachable {
		t.Fatalf("closed server: %+v", got)
	}
}
