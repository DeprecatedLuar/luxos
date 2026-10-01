package nix

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
)

// LockRootNode is the key of a flake.lock's root node.
const LockRootNode = "root"

// LockRef is the identity of a flake input, as originally declared or as
// locked.
type LockRef struct {
	Type  string `json:"type"`
	Owner string `json:"owner"`
	Repo  string `json:"repo"`
	Ref   string `json:"ref"`
	Rev   string `json:"rev"`
	URL   string `json:"url"`
}

// LockNode is one node of a flake.lock. Inputs maps an input name to the
// node key it points at; `follows` entries are skipped.
type LockNode struct {
	Inputs   map[string]string
	Original LockRef
	Locked   LockRef
}

// Lock is a flake.lock: its nodes by key and the names of the root's inputs.
type Lock struct {
	Root  []string
	Nodes map[string]LockNode
}

// ReadLock reads file into a Lock. A missing file returns an empty Lock and no
// error; malformed JSON is an error.
func ReadLock(file string) (Lock, error) {
	lock := Lock{Nodes: map[string]LockNode{}}

	data, err := os.ReadFile(file)
	if err != nil {
		if os.IsNotExist(err) {
			return lock, nil
		}
		return Lock{}, err
	}

	var raw struct {
		Nodes map[string]struct {
			Inputs   map[string]json.RawMessage `json:"inputs"`
			Original LockRef                    `json:"original"`
			Locked   LockRef                    `json:"locked"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return Lock{}, fmt.Errorf("parse %s: %w", file, err)
	}

	for key, n := range raw.Nodes {
		node := LockNode{Inputs: map[string]string{}, Original: n.Original, Locked: n.Locked}
		for name, target := range n.Inputs {
			var nodeKey string
			if err := json.Unmarshal(target, &nodeKey); err != nil {
				continue // a follows array
			}
			node.Inputs[name] = nodeKey
		}
		lock.Nodes[key] = node
	}

	for name := range lock.Nodes[LockRootNode].Inputs {
		lock.Root = append(lock.Root, name)
	}
	sort.Strings(lock.Root)
	return lock, nil
}

// Input returns the node the root input called name points at.
func (l Lock) Input(name string) (LockNode, bool) {
	key, ok := l.Nodes[LockRootNode].Inputs[name]
	if !ok {
		return LockNode{}, false
	}
	node, ok := l.Nodes[key]
	return node, ok
}
