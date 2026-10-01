package nix

import (
	"os"
	"path/filepath"
	"testing"
)

const luxosLockFixture = `{"nodes":{"luxos":{"locked":{"lastModified":1790088510,"narHash":"sha256-7Qp0Ew+0CZeNZjnKSuWI4GTdNllVrPPYzAcTcSGAybw=","owner":"DeprecatedLuar","repo":"luxos","rev":"769dcdb00b27f4aea190db70d1a9d65d858c788d","type":"github"},"original":{"owner":"DeprecatedLuar","ref":"main","repo":"luxos","type":"github"}},"root":{"inputs":{"luxos":"luxos"}}},"root":"root","version":7}`

const lockGraphFixture = `{
  "nodes": {
    "ambxst": {
      "inputs": {"axctl": "axctl", "nixpkgs": "nixpkgs"},
      "locked": {"owner": "Axenide", "repo": "Ambxst", "rev": "1e95", "type": "github"},
      "original": {"owner": "Axenide", "repo": "Ambxst", "type": "github"}
    },
    "axctl": {
      "inputs": {"nixpkgs": ["ambxst", "nixpkgs"]},
      "locked": {"owner": "Axenide", "repo": "axctl", "rev": "86ce", "type": "github"},
      "original": {"owner": "Axenide", "repo": "axctl", "type": "github"}
    },
    "luxos": {
      "inputs": {"nixpkgs": ["nixpkgs"]},
      "locked": {"owner": "DeprecatedLuar", "repo": "luxos", "rev": "769d", "type": "github"},
      "original": {"owner": "DeprecatedLuar", "ref": "main", "repo": "luxos", "type": "github"}
    },
    "nixpkgs": {
      "locked": {"owner": "NixOS", "repo": "nixpkgs", "rev": "8ce4", "type": "github"},
      "original": {"owner": "NixOS", "ref": "nixos-unstable", "repo": "nixpkgs", "type": "github"}
    },
    "root": {"inputs": {"ambxst": "ambxst", "luxos": "luxos", "nixpkgs": "nixpkgs"}}
  },
  "root": "root",
  "version": 7
}`

func writeLock(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "flake.lock")
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestReadLock(t *testing.T) {
	file := filepath.Join(t.TempDir(), "flake.lock")
	if err := os.WriteFile(file, []byte(lockGraphFixture), 0644); err != nil {
		t.Fatal(err)
	}

	g, err := ReadLock(file)
	if err != nil {
		t.Fatal(err)
	}

	wantRoot := []string{"ambxst", "luxos", "nixpkgs"}
	if len(g.Root) != len(wantRoot) {
		t.Fatalf("Root = %v, want %v", g.Root, wantRoot)
	}
	for i := range wantRoot {
		if g.Root[i] != wantRoot[i] {
			t.Errorf("Root = %v, want %v", g.Root, wantRoot)
		}
	}

	if got := g.Nodes["ambxst"].Inputs; got["axctl"] != "axctl" || got["nixpkgs"] != "nixpkgs" {
		t.Errorf("ambxst inputs = %v", got)
	}
	if got := g.Nodes["axctl"].Inputs; len(got) != 0 {
		t.Errorf("axctl follows must be skipped, got %v", got)
	}
	if got := g.Nodes["luxos"].Inputs; len(got) != 0 {
		t.Errorf("luxos follows must be skipped, got %v", got)
	}
	if got := g.Nodes["luxos"].Original.Ref; got != "main" {
		t.Errorf("luxos original ref = %q, want main", got)
	}
	if got := g.Nodes["nixpkgs"].Locked.Rev; got != "8ce4" {
		t.Errorf("nixpkgs locked rev = %q", got)
	}
}

func TestReadLockMissingFile(t *testing.T) {
	g, err := ReadLock(filepath.Join(t.TempDir(), "flake.lock"))
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Root) != 0 || len(g.Nodes) != 0 {
		t.Errorf("want empty graph, got %+v", g)
	}
}

func TestReadLockMalformed(t *testing.T) {
	file := filepath.Join(t.TempDir(), "flake.lock")
	if err := os.WriteFile(file, []byte("{"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadLock(file); err == nil {
		t.Error("want error for malformed JSON")
	}
}

func TestLockInput(t *testing.T) {
	lock, err := ReadLock(writeLock(t, luxosLockFixture))
	if err != nil {
		t.Fatal(err)
	}
	node, ok := lock.Input("luxos")
	if !ok {
		t.Fatal("luxos input not found")
	}
	want := LockRef{Type: "github", Owner: "DeprecatedLuar", Repo: "luxos", Rev: "769dcdb00b27f4aea190db70d1a9d65d858c788d"}
	if node.Locked != want {
		t.Errorf("locked = %+v, want %+v", node.Locked, want)
	}
	if _, ok := lock.Input("nixpkgs"); ok {
		t.Error("absent input reported present")
	}
}

func TestLockInputFollowsRootKey(t *testing.T) {
	lock, err := ReadLock(writeLock(t, `{"nodes":{"luxos_2":{"locked":{"rev":"abc","type":"github"}},"root":{"inputs":{"luxos":"luxos_2"}}},"root":"root","version":7}`))
	if err != nil {
		t.Fatal(err)
	}
	if node, ok := lock.Input("luxos"); !ok || node.Locked.Rev != "abc" {
		t.Errorf("Input(luxos) = %+v, %v; want the node luxos_2", node, ok)
	}
}

func TestReadLockMissingFile_HasNoInputs(t *testing.T) {
	lock, err := ReadLock(filepath.Join(t.TempDir(), "nope.lock"))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := lock.Input("luxos"); ok {
		t.Error("missing file reported an input")
	}
}
