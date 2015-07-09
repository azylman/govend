package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestUpdate(t *testing.T) {
	var cases = []struct {
		desc  string
		cwd   string
		args  []string
		start []*node
		want  []*node
		wdep  Deps
		werr  bool
	}{
		{
			desc: "simple case, update one dependency",
			cwd:  "C",
			args: []string{"D"},
			start: []*node{
				{
					"D",
					"",
					[]*node{
						{"main.go", pkg("D") + decl("D1"), nil},
						{"+git", "D1", nil},
						{"main.go", pkg("D") + decl("D2"), nil},
						{"+git", "D2", nil},
					},
				},
				{
					"C",
					"",
					[]*node{
						{"main.go", pkg("main", "D"), nil},
						{"vendor/Deps.json", deps("C", "D", "D1"), nil},
						{"vendor/D/main.go", pkg("D") + decl("D1"), nil},
						{"+git", "", nil},
					},
				},
			},
			want: []*node{
				{"C/vendor/D/main.go", pkg("D") + decl("D2"), nil},
			},
			wdep: Deps{
				ImportPath: "C",
				Deps: []Dependency{
					{ImportPath: "D", Comment: "D2"},
				},
			},
		},
		{
			desc: "update one dependency, keep other one, no rewrite",
			cwd:  "C",
			args: []string{"D"},
			start: []*node{
				{
					"D",
					"",
					[]*node{
						{"main.go", pkg("D", "E") + decl("D1"), nil},
						{"+git", "D1", nil},
						{"main.go", pkg("D", "E") + decl("D2"), nil},
						{"+git", "D2", nil},
					},
				},
				{
					"E",
					"",
					[]*node{
						{"main.go", pkg("E") + decl("E1"), nil},
						{"+git", "E1", nil},
						{"main.go", pkg("E") + decl("E2"), nil},
						{"+git", "E2", nil},
					},
				},
				{
					"C",
					"",
					[]*node{
						{"main.go", pkg("main", "D", "E"), nil},
						{"vendor/Deps.json", deps("C", "D", "D1", "E", "E1"), nil},
						{"vendor/D/main.go", pkg("D", "E") + decl("D1"), nil},
						{"vendor/E/main.go", pkg("E") + decl("E1"), nil},
						{"+git", "", nil},
					},
				},
			},
			want: []*node{
				{"C/vendor/D/main.go", pkg("D", "E") + decl("D2"), nil},
				{"C/vendor/E/main.go", pkg("E") + decl("E1"), nil},
			},
			wdep: Deps{
				ImportPath: "C",
				Deps: []Dependency{
					{ImportPath: "D", Comment: "D2"},
					{ImportPath: "E", Comment: "E1"},
				},
			},
		},
		{
			desc: "update all dependencies",
			cwd:  "C",
			args: []string{"..."},
			start: []*node{
				{
					"D",
					"",
					[]*node{
						{"main.go", pkg("D") + decl("D1"), nil},
						{"+git", "D1", nil},
						{"main.go", pkg("D") + decl("D2"), nil},
						{"+git", "D2", nil},
					},
				},
				{
					"E",
					"",
					[]*node{
						{"main.go", pkg("E") + decl("E1"), nil},
						{"+git", "E1", nil},
						{"main.go", pkg("E") + decl("E2"), nil},
						{"+git", "E2", nil},
					},
				},
				{
					"C",
					"",
					[]*node{
						{"main.go", pkg("main", "D", "E"), nil},
						{"vendor/Deps.json", deps("C", "D", "D1", "E", "E1"), nil},
						{"vendor/D/main.go", pkg("D") + decl("D1"), nil},
						{"vendor/E/main.go", pkg("E") + decl("E1"), nil},
						{"+git", "", nil},
					},
				},
			},
			want: []*node{
				{"C/vendor/D/main.go", pkg("D") + decl("D2"), nil},
				{"C/vendor/E/main.go", pkg("E") + decl("E2"), nil},
			},
			wdep: Deps{
				ImportPath: "C",
				Deps: []Dependency{
					{ImportPath: "D", Comment: "D2"},
					{ImportPath: "E", Comment: "E2"},
				},
			},
		},
		{
			desc: "one match of two patterns",
			cwd:  "C",
			args: []string{"D", "X"},
			start: []*node{
				{
					"D",
					"",
					[]*node{
						{"main.go", pkg("D") + decl("D1"), nil},
						{"+git", "D1", nil},
						{"main.go", pkg("D") + decl("D2"), nil},
						{"+git", "D2", nil},
					},
				},
				{
					"C",
					"",
					[]*node{
						{"main.go", pkg("main", "D"), nil},
						{"vendor/Deps.json", deps("C", "D", "D1"), nil},
						{"vendor/D/main.go", pkg("D") + decl("D1"), nil},
						{"+git", "", nil},
					},
				},
			},
			want: []*node{
				{"C/vendor/D/main.go", pkg("D") + decl("D2"), nil},
			},
			wdep: Deps{
				ImportPath: "C",
				Deps: []Dependency{
					{ImportPath: "D", Comment: "D2"},
				},
			},
		},
		{
			desc: "no matches",
			cwd:  "C",
			args: []string{"X"},
			start: []*node{
				{
					"D",
					"",
					[]*node{
						{"main.go", pkg("D") + decl("D1"), nil},
						{"+git", "D1", nil},
						{"main.go", pkg("D") + decl("D2"), nil},
						{"+git", "D2", nil},
					},
				},
				{
					"C",
					"",
					[]*node{
						{"main.go", pkg("main", "D"), nil},
						{"vendor/Deps.json", deps("C", "D", "D1"), nil},
						{"vendor/D/main.go", pkg("D") + decl("D1"), nil},
						{"+git", "", nil},
					},
				},
			},
			want: []*node{
				{"C/vendor/D/main.go", pkg("D") + decl("D1"), nil},
			},
			wdep: Deps{
				ImportPath: "C",
				Deps: []Dependency{
					{ImportPath: "D", Comment: "D1"},
				},
			},
			werr: true,
		},
		{
			desc: "update just one package of two in a repo skips it",
			cwd:  "C",
			args: []string{"D/A", "E"},
			start: []*node{
				{
					"D",
					"",
					[]*node{
						{"A/main.go", pkg("A") + decl("D1"), nil},
						{"B/main.go", pkg("B") + decl("D1"), nil},
						{"+git", "D1", nil},
						{"A/main.go", pkg("A") + decl("D2"), nil},
						{"B/main.go", pkg("B") + decl("D2"), nil},
						{"+git", "D2", nil},
					},
				},
				{
					"E",
					"",
					[]*node{
						{"main.go", pkg("E") + decl("E1"), nil},
						{"+git", "E1", nil},
						{"main.go", pkg("E") + decl("E2"), nil},
						{"+git", "E2", nil},
					},
				},
				{
					"C",
					"",
					[]*node{
						{"main.go", pkg("main", "D/A", "D/B", "E"), nil},
						{"vendor/Deps.json", deps("C", "D/A", "D1", "D/B", "D1", "E", "E1"), nil},
						{"vendor/D/A/main.go", pkg("A") + decl("D1"), nil},
						{"vendor/D/B/main.go", pkg("B") + decl("D1"), nil},
						{"vendor/E/main.go", pkg("E") + decl("E1"), nil},
						{"+git", "", nil},
					},
				},
			},
			want: []*node{
				{"C/vendor/D/A/main.go", pkg("A") + decl("D1"), nil},
				{"C/vendor/D/B/main.go", pkg("B") + decl("D1"), nil},
				{"C/vendor/E/main.go", pkg("E") + decl("E2"), nil},
			},
			wdep: Deps{
				ImportPath: "C",
				Deps: []Dependency{
					{ImportPath: "D/A", Comment: "D1"},
					{ImportPath: "D/B", Comment: "D1"},
					{ImportPath: "E", Comment: "E2"},
				},
			},
		},
		{
			desc: "update just one package of two in a repo, none left",
			cwd:  "C",
			args: []string{"D/A"},
			start: []*node{
				{
					"D",
					"",
					[]*node{
						{"A/main.go", pkg("A") + decl("D1"), nil},
						{"B/main.go", pkg("B") + decl("D1"), nil},
						{"+git", "D1", nil},
						{"A/main.go", pkg("A") + decl("D2"), nil},
						{"B/main.go", pkg("B") + decl("D2"), nil},
						{"+git", "D2", nil},
					},
				},
				{
					"C",
					"",
					[]*node{
						{"main.go", pkg("main", "D/A", "D/B"), nil},
						{"vendor/Deps.json", deps("C", "D/A", "D1", "D/B", "D1"), nil},
						{"vendor/D/A/main.go", pkg("A") + decl("D1"), nil},
						{"vendor/D/B/main.go", pkg("B") + decl("D1"), nil},
						{"+git", "", nil},
					},
				},
			},
			want: []*node{
				{"C/vendor/D/A/main.go", pkg("A") + decl("D1"), nil},
				{"C/vendor/D/B/main.go", pkg("B") + decl("D1"), nil},
			},
			wdep: Deps{
				ImportPath: "C",
				Deps: []Dependency{
					{ImportPath: "D/A", Comment: "D1"},
					{ImportPath: "D/B", Comment: "D1"},
				},
			},
			werr: true,
		},
	}

	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	const scratch = "deptest"
	defer os.RemoveAll(scratch)
	for i, test := range cases {
		gopath := filepath.Join(scratch, fmt.Sprintf("%d", i))
		src := filepath.Join(gopath, "src")
		makeTree(t, &node{src, "", test.start}, "")

		dir := filepath.Join(wd, src, test.cwd)
		err = os.Chdir(dir)
		if err != nil {
			panic(err)
		}
		err = os.Setenv("GOPATH", filepath.Join(wd, gopath))
		if err != nil {
			panic(err)
		}
		log.SetOutput(ioutil.Discard)
		err = update(test.args)
		log.SetOutput(os.Stderr)
		if g := err != nil; g != test.werr {
			t.Errorf("update err = %v (%v) want %v", g, err, test.werr)
		}
		err = os.Chdir(wd)
		if err != nil {
			panic(err)
		}

		checkTree(t, &node{src, "", test.want})

		f, err := os.Open(filepath.Join(dir, "vendor/Deps.json"))
		if err != nil {
			t.Error(err)
		}
		g := new(Deps)
		err = json.NewDecoder(f).Decode(g)
		if err != nil {
			t.Error(err)
		}
		f.Close()

		assert.Equal(t, g.ImportPath, test.wdep.ImportPath)
		for i := range g.Deps {
			g.Deps[i].Rev = ""
		}
		assert.Equal(t, g.Deps, test.wdep.Deps)
	}
}
