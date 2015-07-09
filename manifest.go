package main

import (
	"encoding/json"
	"io"
	"os"
	"sort"

	"github.com/azylman/govend/pkgs"
)

const srcdir = "vendor"
const sep = "/" + srcdir + "/"

// Manifest describes what a package needs to be rebuilt reproducibly.
// It's the same information stored in file Deps.
type Manifest struct {
	ImportPath string
	GoVersion  string
	Packages   []string `json:",omitempty"` // Arguments to save, if any.
	Deps       []pkgs.Dependency
}

func ReadManifest(path string, g *Manifest) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	return json.NewDecoder(f).Decode(g)
}

type Deps []pkgs.Dependency

func (d Deps) Len() int           { return len(d) }
func (d Deps) Swap(i, j int)      { d[i], d[j] = d[j], d[i] }
func (d Deps) Less(i, j int) bool { return d[i].ImportPath < d[j].ImportPath }

func (g *Manifest) sortDeps() {
	sort.Sort(Deps(g.Deps))
}

func (g *Manifest) WriteTo(w io.Writer) (int64, error) {
	// Make sure we're writing in a consistent order
	g.sortDeps()

	b, err := json.MarshalIndent(g, "", "\t")
	if err != nil {
		return 0, err
	}
	n, err := w.Write(append(b, '\n'))
	return int64(n), err
}
