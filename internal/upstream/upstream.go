// Package upstream is the network adapter for asking where a locked flake
// input's source currently points: the GitHub commit feed over HTTP and
// `git ls-remote`. It never prints and never resolves paths.
package upstream

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/DeprecatedLuar/luxos/internal/staging"
)

const (
	typeGitHub = "github"
	typeGit    = "git"

	// defaultRef resolves to the repository's default branch.
	defaultRef = "HEAD"

	requestTimeout = 10 * time.Second
	feedURLFormat  = "%s/%s/%s/commits/%s.atom"
	headsRefFormat = "refs/heads/%s"
)

// githubBase is the GitHub site root; a variable so tests can point it at a
// local server.
var githubBase = "https://github.com"

// ErrUnsupported is returned for an input type Tip cannot query.
var ErrUnsupported = errors.New("upstream: unsupported input type")

var feedCommitRe = regexp.MustCompile(`Commit/([0-9a-f]{40})`)

var httpClient = &http.Client{Timeout: requestTimeout}

// Tip returns the rev the input's tracked ref currently points at upstream.
func Tip(ref staging.LockRef) (string, error) {
	switch ref.Type {
	case typeGitHub:
		return githubTip(ref)
	case typeGit:
		return gitTip(ref)
	default:
		return "", ErrUnsupported
	}
}

func githubTip(ref staging.LockRef) (string, error) {
	branch := ref.Ref
	if branch == "" {
		branch = defaultRef
	}
	url := fmt.Sprintf(feedURLFormat, githubBase, ref.Owner, ref.Repo, branch)

	resp, err := httpClient.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("upstream: GET %s: %s", url, resp.Status)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	m := feedCommitRe.FindSubmatch(body)
	if m == nil {
		return "", fmt.Errorf("upstream: no commit in feed %s", url)
	}
	return string(m[1]), nil
}

func gitTip(ref staging.LockRef) (string, error) {
	target := defaultRef
	if ref.Ref != "" {
		target = fmt.Sprintf(headsRefFormat, ref.Ref)
	}
	out, err := exec.Command("git", "ls-remote", ref.URL, target).Output()
	if err != nil {
		return "", fmt.Errorf("upstream: git ls-remote %s: %w", ref.URL, err)
	}
	fields := strings.Fields(string(out))
	if len(fields) == 0 {
		return "", fmt.Errorf("upstream: git ls-remote %s %s: no such ref", ref.URL, target)
	}
	return fields[0], nil
}
