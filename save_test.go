package main

import (
	"bytes"
	"encoding/json"
	"io/ioutil"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"text/template"

	"github.com/azylman/govend/pkgs"
	"github.com/stretchr/testify/assert"
)

// node represents a file tree or a VCS repo
type node struct {
	path    string      // file name or commit type
	body    interface{} // file contents or commit tag
	entries []*node     // nil if the entry is a file
}

var (
	pkgtpl = template.Must(template.New("package").Parse(`package {{.Name}}

import (
{{range .Imports}}	{{printf "%q" .}}
{{end}})
`))
)

func pkg(name string, pkg ...string) string {
	v := struct {
		Name    string
		Imports []string
	}{name, pkg}
	var buf bytes.Buffer
	if err := pkgtpl.Execute(&buf, v); err != nil {
		panic(err)
	}
	return buf.String()
}

func decl(name string) string {
	return "var " + name + " int\n"
}

func deps(importpath string, keyval ...string) *Manifest {
	g := &Manifest{
		ImportPath: importpath,
	}
	for i := 0; i < len(keyval); i += 2 {
		g.Deps = append(g.Deps, pkgs.Dependency{
			ImportPath: keyval[i],
			Comment:    keyval[i+1],
		})
	}
	return g
}

func TestSave(t *testing.T) {
	var cases = []struct {
		desc     string
		cwd      string
		start    []*node
		altstart []*node
		want     []*node
		wdep     Manifest
		werr     bool
	}{
		{
			desc: "simple case, one dependency",
			cwd:  "C",
			start: []*node{
				{
					"C",
					"",
					[]*node{
						{"main.go", pkg("main", "D"), nil},
						{"+git", "", nil},
					},
				},
				{
					"D",
					"",
					[]*node{
						{"main.go", pkg("D"), nil},
						{"+git", "D1", nil},
					},
				},
			},
			want: []*node{
				{"C/main.go", pkg("main", "D"), nil},
				{"C/vendor/D/main.go", pkg("D"), nil},
			},
			wdep: Manifest{
				ImportPath: "C",
				Deps: []pkgs.Dependency{
					{ImportPath: "D", Comment: "D1"},
				},
			},
		},
		{
			desc: "strip import comment",
			cwd:  "C",
			start: []*node{
				{
					"C",
					"",
					[]*node{
						{"main.go", pkg("main", "D"), nil},
						{"+git", "", nil},
					},
				},
				{
					"D",
					"",
					[]*node{
						{"main.go", `package D // import "D"`, nil},
						{"+git", "D1", nil},
					},
				},
			},
			want: []*node{
				{"C/main.go", pkg("main", "D"), nil},
				{"C/vendor/D/main.go", "package D\n", nil},
			},
			wdep: Manifest{
				ImportPath: "C",
				Deps: []pkgs.Dependency{
					{ImportPath: "D", Comment: "D1"},
				},
			},
		},
		{
			// see bug https://github.com/tools/godep/issues/69
			desc: "dependency in same repo with existing manifest",
			cwd:  "P",
			start: []*node{
				{
					"P",
					"",
					[]*node{
						{"main.go", pkg("P", "P/Q"), nil},
						{"Q/main.go", pkg("Q"), nil},
						{"vendor/Deps.json", `{}`, nil},
						{"+git", "C1", nil},
					},
				},
			},
			want: []*node{
				{"P/main.go", pkg("P", "P/Q"), nil},
				{"P/Q/main.go", pkg("Q"), nil},
			},
			wdep: Manifest{
				ImportPath: "P",
				Deps:       []pkgs.Dependency{},
			},
		},
		{
			// see bug https://github.com/tools/godep/issues/70
			desc: "dependency on parent directory in same repo",
			cwd:  "P",
			start: []*node{
				{
					"P",
					"",
					[]*node{
						{"main.go", pkg("P"), nil},
						{"Q/main.go", pkg("Q", "P"), nil},
						{"+git", "C1", nil},
					},
				},
			},
			want: []*node{
				{"P/main.go", pkg("P"), nil},
				{"P/Q/main.go", pkg("Q", "P"), nil},
			},
			wdep: Manifest{
				ImportPath: "P",
				Deps:       []pkgs.Dependency{},
			},
		},
		{
			desc: "transitive dependency",
			cwd:  "C",
			start: []*node{
				{
					"C",
					"",
					[]*node{
						{"main.go", pkg("main", "D"), nil},
						{"+git", "", nil},
					},
				},
				{
					"D",
					"",
					[]*node{
						{"main.go", pkg("D", "T"), nil},
						{"+git", "D1", nil},
					},
				},
				{
					"T",
					"",
					[]*node{
						{"main.go", pkg("T"), nil},
						{"+git", "T1", nil},
					},
				},
			},
			want: []*node{
				{"C/main.go", pkg("main", "D"), nil},
				{"C/vendor/D/main.go", pkg("D", "T"), nil},
				{"C/vendor/T/main.go", pkg("T"), nil},
			},
			wdep: Manifest{
				ImportPath: "C",
				Deps: []pkgs.Dependency{
					{ImportPath: "D", Comment: "D1"},
					{ImportPath: "T", Comment: "T1"},
				},
			},
		},
		{
			desc: "two packages, one in a subdirectory",
			cwd:  "C",
			start: []*node{
				{
					"C",
					"",
					[]*node{
						{"main.go", pkg("main", "D", "D/P"), nil},
						{"+git", "", nil},
					},
				},
				{
					"D",
					"",
					[]*node{
						{"main.go", pkg("D"), nil},
						{"P/main.go", pkg("P"), nil},
						{"+git", "D1", nil},
					},
				},
			},
			want: []*node{
				{"C/main.go", pkg("main", "D", "D/P"), nil},
				{"C/vendor/D/main.go", pkg("D"), nil},
				{"C/vendor/D/P/main.go", pkg("P"), nil},
			},
			wdep: Manifest{
				ImportPath: "C",
				Deps: []pkgs.Dependency{
					{ImportPath: "D", Comment: "D1"},
				},
			},
		},
		{
			desc: "repo root is not a package (no go files)",
			cwd:  "C",
			start: []*node{
				{
					"C",
					"",
					[]*node{
						{"main.go", pkg("main", "D/P", "D/Q"), nil},
						{"+git", "", nil},
					},
				},
				{
					"D",
					"",
					[]*node{
						{"P/main.go", pkg("P"), nil},
						{"Q/main.go", pkg("Q"), nil},
						{"+git", "D1", nil},
					},
				},
			},
			want: []*node{
				{"C/main.go", pkg("main", "D/P", "D/Q"), nil},
				{"C/vendor/D/P/main.go", pkg("P"), nil},
				{"C/vendor/D/Q/main.go", pkg("Q"), nil},
			},
			wdep: Manifest{
				ImportPath: "C",
				Deps: []pkgs.Dependency{
					{ImportPath: "D/P", Comment: "D1"},
					{ImportPath: "D/Q", Comment: "D1"},
				},
			},
		},
		{
			desc: "symlink",
			cwd:  "C",
			start: []*node{
				{
					"C",
					"",
					[]*node{
						{"main.x", pkg("main", "D"), nil},
						{"main.go", "symlink:main.x", nil},
						{"+git", "", nil},
					},
				},
				{
					"D",
					"",
					[]*node{
						{"main.go", pkg("D"), nil},
						{"+git", "D1", nil},
					},
				},
			},
			want: []*node{
				{"C/main.go", pkg("main", "D"), nil},
				{"C/vendor/D/main.go", pkg("D"), nil},
			},
			wdep: Manifest{
				ImportPath: "C",
				Deps: []pkgs.Dependency{
					{ImportPath: "D", Comment: "D1"},
				},
			},
		},
		{
			desc: "add one dependency; keep other dependency version",
			cwd:  "C",
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
						{"main.go", pkg("E"), nil},
						{"+git", "E1", nil},
					},
				},
				{
					"C",
					"",
					[]*node{
						{"main.go", pkg("main", "D", "E"), nil},
						{"vendor/Deps.json", deps("C", "D", "D1"), nil},
						{"vendor/D/main.go", pkg("D") + decl("D1"), nil},
						{"+git", "", nil},
					},
				},
			},
			want: []*node{
				{"C/main.go", pkg("main", "D", "E"), nil},
				{"C/vendor/D/main.go", pkg("D") + decl("D1"), nil},
				{"C/vendor/E/main.go", pkg("E"), nil},
			},
			wdep: Manifest{
				ImportPath: "C",
				Deps: []pkgs.Dependency{
					{ImportPath: "D", Comment: "D1"},
					{ImportPath: "E", Comment: "E1"},
				},
			},
		},
		{
			desc: "remove one dependency; keep other dependency version",
			cwd:  "C",
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
					},
				},
				{
					"C",
					"",
					[]*node{
						{"main.go", pkg("main", "D"), nil},
						{"vendor/Deps.json", deps("C", "D", "D1", "E", "E1"), nil},
						{"vendor/D/main.go", pkg("D") + decl("D1"), nil},
						{"vendor/E/main.go", pkg("E") + decl("E1"), nil},
						{"+git", "", nil},
					},
				},
			},
			want: []*node{
				{"C/vendor/D/main.go", pkg("D") + decl("D1"), nil},
				{"C/vendor/E/main.go", "(absent)", nil},
			},
			wdep: Manifest{
				ImportPath: "C",
				Deps: []pkgs.Dependency{
					{ImportPath: "D", Comment: "D1"},
				},
			},
		},
		{
			desc: "add one dependency from same repo",
			cwd:  "C",
			start: []*node{
				{
					"D",
					"",
					[]*node{
						{"A/main.go", pkg("A") + decl("A1"), nil},
						{"B/main.go", pkg("B") + decl("B1"), nil},
						{"+git", "D1", nil},
					},
				},
				{
					"C",
					"",
					[]*node{
						{"main.go", pkg("main", "D/A", "D/B"), nil},
						{"vendor/Deps.json", deps("C", "D/A", "D1"), nil},
						{"vendor/D/A/main.go", pkg("A") + decl("A1"), nil},
						{"+git", "", nil},
					},
				},
			},
			want: []*node{
				{"C/vendor/D/A/main.go", pkg("A") + decl("A1"), nil},
				{"C/vendor/D/B/main.go", pkg("B") + decl("B1"), nil},
			},
			wdep: Manifest{
				ImportPath: "C",
				Deps: []pkgs.Dependency{
					{ImportPath: "D/A", Comment: "D1"},
					{ImportPath: "D/B", Comment: "D1"},
				},
			},
		},
		{
			desc: "add one dependency from same repo, require same version",
			cwd:  "C",
			start: []*node{
				{
					"D",
					"",
					[]*node{
						{"A/main.go", pkg("A") + decl("A1"), nil},
						{"B/main.go", pkg("B") + decl("B1"), nil},
						{"+git", "D1", nil},
						{"A/main.go", pkg("A") + decl("A2"), nil},
						{"B/main.go", pkg("B") + decl("B2"), nil},
						{"+git", "D2", nil},
					},
				},
				{
					"C",
					"",
					[]*node{
						{"main.go", pkg("main", "D/A", "D/B"), nil},
						{"vendor/Deps.json", deps("C", "D/A", "D1"), nil},
						{"vendor/D/A/main.go", pkg("A") + decl("A1"), nil},
						{"+git", "", nil},
					},
				},
			},
			want: []*node{
				{"C/vendor/D/A/main.go", pkg("A") + decl("A1"), nil},
			},
			wdep: Manifest{
				ImportPath: "C",
				Deps: []pkgs.Dependency{
					{ImportPath: "D/A", Comment: "D1"},
				},
			},
			werr: true,
		},
		{
			desc: "replace dependency from same repo parent dir",
			cwd:  "C",
			start: []*node{
				{
					"D",
					"",
					[]*node{
						{"main.go", pkg("D") + decl("D1"), nil},
						{"A/main.go", pkg("A") + decl("A1"), nil},
						{"+git", "D1", nil},
					},
				},
				{
					"C",
					"",
					[]*node{
						{"main.go", pkg("main", "D"), nil},
						{"vendor/Deps.json", deps("C", "D/A", "D1"), nil},
						{"vendor/D/A/main.go", pkg("A") + decl("A1"), nil},
						{"+git", "", nil},
					},
				},
			},
			want: []*node{
				{"C/vendor/D/main.go", pkg("D") + decl("D1"), nil},
				{"C/vendor/D/A/main.go", pkg("A") + decl("A1"), nil},
			},
			wdep: Manifest{
				ImportPath: "C",
				Deps: []pkgs.Dependency{
					{ImportPath: "D", Comment: "D1"},
				},
			},
		},
		{
			desc: "replace dependency from same repo parent dir, require same version",
			cwd:  "C",
			start: []*node{
				{
					"D",
					"",
					[]*node{
						{"main.go", pkg("D") + decl("D1"), nil},
						{"A/main.go", pkg("A") + decl("A1"), nil},
						{"+git", "D1", nil},
						{"main.go", pkg("D") + decl("D2"), nil},
						{"A/main.go", pkg("A") + decl("A2"), nil},
						{"+git", "D2", nil},
					},
				},
				{
					"C",
					"",
					[]*node{
						{"main.go", pkg("main", "D"), nil},
						{"vendor/Deps.json", deps("C", "D/A", "D1"), nil},
						{"vendor/D/A/main.go", pkg("A") + decl("A1"), nil},
						{"+git", "", nil},
					},
				},
			},
			want: []*node{
				{"C/vendor/D/A/main.go", pkg("A") + decl("A2"), nil},
			},
			wdep: Manifest{
				ImportPath: "C",
				Deps: []pkgs.Dependency{
					{ImportPath: "D", Comment: "D2"},
				},
			},
		},
		{
			desc: "replace dependency from same repo child dir",
			cwd:  "C",
			start: []*node{
				{
					"D",
					"",
					[]*node{
						{"main.go", pkg("D") + decl("D1"), nil},
						{"A/main.go", pkg("A") + decl("A1"), nil},
						{"+git", "D1", nil},
					},
				},
				{
					"C",
					"",
					[]*node{
						{"main.go", pkg("main", "D/A"), nil},
						{"vendor/Deps.json", deps("C", "D", "D1"), nil},
						{"vendor/D/main.go", pkg("D") + decl("D1"), nil},
						{"vendor/D/A/main.go", pkg("A") + decl("A1"), nil},
						{"+git", "", nil},
					},
				},
			},
			want: []*node{
				{"C/vendor/D/main.go", "(absent)", nil},
				{"C/vendor/D/A/main.go", pkg("A") + decl("A1"), nil},
			},
			wdep: Manifest{
				ImportPath: "C",
				Deps: []pkgs.Dependency{
					{ImportPath: "D/A", Comment: "D1"},
				},
			},
		},
		{
			desc: "replace dependency from same repo child dir, require same version",
			cwd:  "C",
			start: []*node{
				{
					"D",
					"",
					[]*node{
						{"main.go", pkg("D") + decl("D1"), nil},
						{"A/main.go", pkg("A") + decl("A1"), nil},
						{"+git", "D1", nil},
						{"main.go", pkg("D") + decl("D2"), nil},
						{"A/main.go", pkg("A") + decl("A2"), nil},
						{"+git", "D2", nil},
					},
				},
				{
					"C",
					"",
					[]*node{
						{"main.go", pkg("main", "D/A"), nil},
						{"vendor/Deps.json", deps("C", "D", "D1"), nil},
						{"vendor/D/A/main.go", pkg("A") + decl("A1"), nil},
						{"+git", "", nil},
					},
				},
			},
			want: []*node{
				{"C/vendor/D/A/main.go", pkg("A") + decl("A2"), nil},
			},
			wdep: Manifest{
				ImportPath: "C",
				Deps: []pkgs.Dependency{
					{ImportPath: "D/A", Comment: "D2"},
				},
			},
		},
		{
			desc: "Bug https://github.com/tools/godep/issues/85",
			cwd:  "C",
			start: []*node{
				{
					"D",
					"",
					[]*node{
						{"A/main.go", pkg("A") + decl("A1"), nil},
						{"B/main.go", pkg("B") + decl("B1"), nil},
						{"+git", "D1", nil},
						{"A/main.go", pkg("A") + decl("A2"), nil},
						{"B/main.go", pkg("B") + decl("B2"), nil},
						{"+git", "D2", nil},
					},
				},
				{
					"C",
					"",
					[]*node{
						{"main.go", pkg("main", "D/A", "D/B"), nil},
						{"vendor/Deps.json", deps("C", "D/A", "D1", "D/B", "D1"), nil},
						{"vendor/D/A/main.go", pkg("A") + decl("A1"), nil},
						{"vendor/D/B/main.go", pkg("B") + decl("B1"), nil},
						{"+git", "", nil},
					},
				},
			},
			want: []*node{
				{"C/vendor/D/A/main.go", pkg("A") + decl("A1"), nil},
				{"C/vendor/D/B/main.go", pkg("B") + decl("B1"), nil},
			},
			wdep: Manifest{
				ImportPath: "C",
				Deps: []pkgs.Dependency{
					{ImportPath: "D/A", Comment: "D1"},
					{ImportPath: "D/B", Comment: "D1"},
				},
			},
		},
	}

	wd, err := os.Getwd()
	assert.Nil(t, err)
	const scratch = "deptest"
	defer os.RemoveAll(scratch)
	for _, test := range cases {
		t.Logf("desc: %s", test.desc)
		assert.Nil(t, os.RemoveAll(scratch))
		altsrc := filepath.Join(scratch, "r2", "src")
		if test.altstart != nil {
			makeTree(t, &node{altsrc, "", test.altstart}, "")
		}
		src := filepath.Join(scratch, "r1", "src")
		makeTree(t, &node{src, "", test.start}, altsrc)

		dir := filepath.Join(wd, src, test.cwd)
		if err := os.Chdir(dir); err != nil {
			panic(err)
		}
		root1 := filepath.Join(wd, scratch, "r1")
		root2 := filepath.Join(wd, scratch, "r2")
		if err := os.Setenv("GOPATH", root1+string(os.PathListSeparator)+root2); err != nil {
			panic(err)
		}
		if test.werr {
			assert.NotNil(t, save([]string{}))
		} else {
			if err := save([]string{}); err != nil {
				t.Fatalf("got unexpected error %s", err.Error())
			}
		}
		if err := os.Chdir(wd); err != nil {
			panic(err)
		}

		checkTree(t, &node{src, "", test.want})

		f, err := os.Open(filepath.Join(dir, "vendor/Deps.json"))
		assert.Nil(t, err)
		g := new(Manifest)
		assert.Nil(t, json.NewDecoder(f).Decode(g))
		f.Close()

		assert.Equal(t, g.ImportPath, test.wdep.ImportPath)
		for i := range g.Deps {
			g.Deps[i].Rev = ""
		}
		assert.Equal(t, test.wdep.Deps, g.Deps)
	}
}

func makeTree(t *testing.T, tree *node, altpath string) (gopath string) {
	walkTree(tree, tree.path, func(path string, n *node) {
		g, isDeps := n.body.(*Manifest)
		body, _ := n.body.(string)
		switch {
		case isDeps:
			for i, dep := range g.Deps {
				rel := filepath.FromSlash(dep.ImportPath)
				dir := filepath.Join(tree.path, rel)
				if _, err := os.Stat(dir); os.IsNotExist(err) {
					dir = filepath.Join(altpath, rel)
				}
				tag := dep.Comment
				rev := strings.TrimSpace(run(t, dir, "git", "rev-parse", tag))
				g.Deps[i].Rev = rev
			}
			os.MkdirAll(filepath.Dir(path), 0770)
			f, err := os.Create(path)
			assert.Nil(t, err)
			defer f.Close()
			assert.Nil(t, json.NewEncoder(f).Encode(g))
		case n.path == "+git":
			dir := filepath.Dir(path)
			run(t, dir, "git", "init") // repo might already exist, but ok
			run(t, dir, "git", "add", ".")
			run(t, dir, "git", "commit", "-m", "govend")
			if body != "" {
				run(t, dir, "git", "tag", body)
			}
		case n.entries == nil && strings.HasPrefix(body, "symlink:"):
			target := strings.TrimPrefix(body, "symlink:")
			os.Symlink(target, path)
		case n.entries == nil && body == "(absent)":
			panic("is this gonna be forever")
		case n.entries == nil:
			os.MkdirAll(filepath.Dir(path), 0770)
			assert.Nil(t, ioutil.WriteFile(path, []byte(body), 0660))
		default:
			os.MkdirAll(path, 0770)
		}
	})
	return gopath
}

func checkTree(t *testing.T, want *node) {
	walkTree(want, want.path, func(path string, n *node) {
		body := n.body.(string)
		switch {
		case n.path == "+git":
			panic("is this real life")
		case n.entries == nil && strings.HasPrefix(body, "symlink:"):
			panic("why is this happening to me")
		case n.entries == nil && body == "(absent)":
			body, err := ioutil.ReadFile(path)
			if !os.IsNotExist(err) {
				t.Errorf("checkTree: %s = %s want absent", path, string(body))
				return
			}
		case n.entries == nil:
			gbody, err := ioutil.ReadFile(path)
			assert.Nil(t, err)
			assert.Equal(t, body, string(gbody))
		default:
			os.MkdirAll(path, 0770)
		}
	})
}

func walkTree(n *node, path string, f func(path string, n *node)) {
	f(path, n)
	for _, e := range n.entries {
		walkTree(e, filepath.Join(path, filepath.FromSlash(e.path)), f)
	}
}

func run(t *testing.T, dir, name string, args ...string) string {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		panic(name + " " + strings.Join(args, " ") + ": " + err.Error())
	}
	return string(out)
}

func TestStripImportComment(t *testing.T) {
	var cases = []struct{ s, w string }{
		{`package foo`, `package foo`},
		{`anything else`, `anything else`},
		{`package foo // import "bar/foo"`, `package foo`},
		{`package foo /* import "bar/foo" */`, `package foo`},
		{`package  foo  //  import  "bar/foo" `, `package  foo`},
		{"package foo // import `bar/foo`", `package foo`},
		{`package foo /* import "bar/foo" */; var x int`, `package foo; var x int`},
		{`package foo // import "bar/foo" garbage`, `package foo // import "bar/foo" garbage`},
		{`package xpackage foo // import "bar/foo"`, `package xpackage foo // import "bar/foo"`},
	}

	for _, test := range cases {
		assert.Equal(t, string(stripImportComment([]byte(test.s))), test.w)
	}
}

func TestCopyWithoutImportCommentLongLines(t *testing.T) {
	tmp := make([]byte, int(math.Pow(2, 16)))
	for i, _ := range tmp {
		tmp[i] = 111 // fill it with "o"s
	}

	iStr := `package foo` + string(tmp) + `\n`

	o := new(bytes.Buffer)
	i := strings.NewReader(iStr)
	assert.Nil(t, copyWithoutImportComment(o, i))
}
