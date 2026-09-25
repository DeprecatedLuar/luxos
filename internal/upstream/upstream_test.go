package upstream

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DeprecatedLuar/luxos/internal/staging"
)

const tipRev = "b6018f87aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func serve(t *testing.T, body string, status int) *[]string {
	t.Helper()
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	old := githubBase
	githubBase = srv.URL
	t.Cleanup(func() { githubBase = old; srv.Close() })
	return &paths
}

func TestTipGitHub(t *testing.T) {
	paths := serve(t, `<feed><entry><id>tag:github.com,2008:Grit::Commit/`+tipRev+`</id></entry>
<entry><id>tag:github.com,2008:Grit::Commit/1111111111111111111111111111111111111111</id></entry></feed>`, 200)

	got, err := Tip(staging.LockRef{Type: "github", Owner: "o", Repo: "r", Ref: "nixos-25.11"})
	if err != nil || got != tipRev {
		t.Fatalf("got %q, %v", got, err)
	}
	if (*paths)[0] != "/o/r/commits/nixos-25.11.atom" {
		t.Errorf("path %q", (*paths)[0])
	}

	if _, err := Tip(staging.LockRef{Type: "github", Owner: "o", Repo: "r"}); err != nil {
		t.Fatal(err)
	}
	if (*paths)[1] != "/o/r/commits/HEAD.atom" {
		t.Errorf("default path %q", (*paths)[1])
	}
}

func TestTipGitHubBadFeed(t *testing.T) {
	serve(t, `<feed>nothing</feed>`, 200)
	if _, err := Tip(staging.LockRef{Type: "github", Owner: "o", Repo: "r"}); err == nil {
		t.Fatal("expected parse error")
	}
}

func TestTipGitHubStatus(t *testing.T) {
	serve(t, ``, 404)
	if _, err := Tip(staging.LockRef{Type: "github", Owner: "o", Repo: "r"}); err == nil {
		t.Fatal("expected status error")
	}
}

func TestTipUnsupported(t *testing.T) {
	if _, err := Tip(staging.LockRef{Type: "path"}); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("got %v", err)
	}
}
