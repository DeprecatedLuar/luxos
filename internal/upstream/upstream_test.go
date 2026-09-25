package upstream

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os/exec"
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

func serveAPI(t *testing.T, body string, status int) *[]string {
	t.Helper()
	var uris []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		uris = append(uris, r.URL.RequestURI())
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	old := githubAPIBase
	githubAPIBase = srv.URL
	t.Cleanup(func() { githubAPIBase = old; srv.Close() })
	return &uris
}

func TestTagsGitHub(t *testing.T) {
	uris := serveAPI(t, `[{"name":"1.3.8","commit":{"sha":"aaa"}},{"name":"1.3.4","commit":{"sha":"bbb"}},{"name":"dup","commit":{"sha":"aaa"}}]`, 200)
	got, err := Tags(staging.LockRef{Type: "github", Owner: "o", Repo: "r"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got["aaa"] != "1.3.8" || got["bbb"] != "1.3.4" {
		t.Errorf("tags %v", got)
	}
	if (*uris)[0] != "/repos/o/r/tags?per_page=100" {
		t.Errorf("uri %q", (*uris)[0])
	}
}

func TestTagsGitHubStatus(t *testing.T) {
	serveAPI(t, `{"message":"rate limit"}`, 403)
	if _, err := Tags(staging.LockRef{Type: "github", Owner: "o", Repo: "r"}); err == nil {
		t.Fatal("want error")
	}
}

func TestTagsUnsupported(t *testing.T) {
	if _, err := Tags(staging.LockRef{Type: "tarball"}); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("got %v", err)
	}
}

func TestTagsGit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	run := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@t"}, args...)...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return string(out)
	}
	run("init", "-q")
	run("commit", "-q", "--allow-empty", "-m", "one")
	run("tag", "light")
	run("commit", "-q", "--allow-empty", "-m", "two")
	run("tag", "-a", "annotated", "-m", "ann")
	lightRev := trimNL(run("rev-parse", "light"))
	annRev := trimNL(run("rev-parse", "annotated^{commit}"))

	got, err := Tags(staging.LockRef{Type: "git", URL: dir})
	if err != nil {
		t.Fatal(err)
	}
	if got[lightRev] != "light" || got[annRev] != "annotated" || len(got) != 2 {
		t.Errorf("tags %v (light %s, annotated %s)", got, lightRev, annRev)
	}
}

func trimNL(s string) string {
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	return s
}

func TestCompareGitHub(t *testing.T) {
	uris := serveAPI(t, `{"ahead_by":55,"commits":[{"sha":"c1"},{"sha":"c2"}]}`, 200)
	ahead, shas, err := Compare(staging.LockRef{Type: "github", Owner: "o", Repo: "r"}, "base", "head")
	if err != nil || ahead != 55 || len(shas) != 2 || shas[1] != "c2" {
		t.Fatalf("got %d %v %v", ahead, shas, err)
	}
	if (*uris)[0] != "/repos/o/r/compare/base...head" {
		t.Errorf("uri %q", (*uris)[0])
	}
}

func TestCompareErrors(t *testing.T) {
	if _, _, err := Compare(staging.LockRef{Type: "git"}, "a", "b"); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("got %v", err)
	}
	serveAPI(t, ``, 429)
	if _, _, err := Compare(staging.LockRef{Type: "github", Owner: "o", Repo: "r"}, "a", "b"); err == nil {
		t.Fatal("want error")
	}
}
