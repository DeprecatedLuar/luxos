// Package upstream is the network adapter for asking where a locked flake
// input's source currently points: the GitHub commit feed and REST API over
// HTTP (tip, tags, compare) and `git ls-remote`. It never prints and never resolves paths.
package upstream

import (
	"encoding/json"
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

	tagsURLFormat    = "%s/repos/%s/%s/tags?per_page=100"
	compareURLFormat = "%s/repos/%s/%s/compare/%s...%s"
	tagsRefPrefix    = "refs/tags/"
	peeledSuffix     = "^{}"
	apiAccept        = "application/vnd.github+json"
)

// githubBase is the GitHub site root; a variable so tests can point it at a
// local server.
var githubBase = "https://github.com"

// githubAPIBase is the GitHub REST API root; a variable so tests can point it
// at a local server.
var githubAPIBase = "https://api.github.com"

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

// apiGet GETs a GitHub REST URL and decodes the JSON body into out. Any
// non-2xx status, rate limiting included, is an error.
func apiGet(url string, out any) error {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", apiAccept)
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("upstream: GET %s: %s", url, resp.Status)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("upstream: decode %s: %w", url, err)
	}
	return nil
}

// Tags returns the input's tags as rev -> tag name. When several tags share a
// rev, the first the source lists wins.
func Tags(ref staging.LockRef) (map[string]string, error) {
	switch ref.Type {
	case typeGitHub:
		return githubTags(ref)
	case typeGit:
		return gitTags(ref)
	default:
		return nil, ErrUnsupported
	}
}

func githubTags(ref staging.LockRef) (map[string]string, error) {
	url := fmt.Sprintf(tagsURLFormat, githubAPIBase, ref.Owner, ref.Repo)
	var list []struct {
		Name   string `json:"name"`
		Commit struct {
			SHA string `json:"sha"`
		} `json:"commit"`
	}
	if err := apiGet(url, &list); err != nil {
		return nil, err
	}
	tags := map[string]string{}
	for _, t := range list {
		if _, taken := tags[t.Commit.SHA]; !taken {
			tags[t.Commit.SHA] = t.Name
		}
	}
	return tags, nil
}

func gitTags(ref staging.LockRef) (map[string]string, error) {
	out, err := exec.Command("git", "ls-remote", "--tags", ref.URL).Output()
	if err != nil {
		return nil, fmt.Errorf("upstream: git ls-remote --tags %s: %w", ref.URL, err)
	}

	var names []string
	revOf := map[string]string{}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 || !strings.HasPrefix(fields[1], tagsRefPrefix) {
			continue
		}
		name := strings.TrimPrefix(fields[1], tagsRefPrefix)
		peeled := strings.HasSuffix(name, peeledSuffix)
		name = strings.TrimSuffix(name, peeledSuffix)
		if _, seen := revOf[name]; !seen {
			names = append(names, name)
		}
		if _, seen := revOf[name]; !seen || peeled {
			revOf[name] = fields[0]
		}
	}

	tags := map[string]string{}
	for _, name := range names {
		if _, taken := tags[revOf[name]]; !taken {
			tags[revOf[name]] = name
		}
	}
	return tags, nil
}

// Compare returns how many commits head is ahead of base and their revs,
// oldest first. GitHub only; the API lists at most 250 commits, ahead is the
// full count.
func Compare(ref staging.LockRef, base, head string) (ahead int, shas []string, err error) {
	if ref.Type != typeGitHub {
		return 0, nil, ErrUnsupported
	}
	url := fmt.Sprintf(compareURLFormat, githubAPIBase, ref.Owner, ref.Repo, base, head)
	var cmp struct {
		AheadBy int `json:"ahead_by"`
		Commits []struct {
			SHA string `json:"sha"`
		} `json:"commits"`
	}
	if err := apiGet(url, &cmp); err != nil {
		return 0, nil, err
	}
	for _, c := range cmp.Commits {
		shas = append(shas, c.SHA)
	}
	return cmp.AheadBy, shas, nil
}
