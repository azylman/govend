package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/kr/fs"
)

var cmdSave = &Command{
	Usage: "save",
	Short: "list and copy dependencies into Godeps",
	Long: `
Save writes a list of the dependencies of the named packages along
with the exact source control revision of each dependency, and copies
their source code into a subdirectory.

The list is written to vendor/Godeps.json, and source code for all
dependencies is copied into vendor/

The dependency list is a JSON document with the following structure:

	type Godeps struct {
		ImportPath string
		GoVersion  string   // Abridged output of 'go version'.
		Packages   []string // Arguments to godep save, if any.
		Deps       []struct {
			ImportPath string
			Comment    string // Tag or description of commit.
			Rev        string // VCS-specific commit ID.
		}
	}

Any dependencies already present in the list will be left unchanged.
To update a dependency to a newer revision, use 'godep update'.

For more about specifying packages, see 'go help packages'.
`,
	Run: runSave,
}

func runSave(cmd *Command, args []string) {
	err := save()
	if err != nil {
		log.Fatalln(err)
	}
}

func save() error {
	dot, err := LoadPackages("./...")
	if err != nil {
		return err
	}

	ver, err := goVersion()
	if err != nil {
		return err
	}

	gnew := &Godeps{
		ImportPath: dot[0].ImportPath,
		GoVersion:  ver,
	}
	if gnew.Load(dot); err != nil {
		return err
	}
	if gnew.Deps == nil {
		gnew.Deps = make([]Dependency, 0) // produce json [], not null
	}
	gdisk := copyGodeps(gnew)

	var gold Godeps
	oldIsFile, err := readOldGodeps(&gold)
	if err != nil {
		return err
	}
	if err := carryVersions(&gold, gnew); err != nil {
		return err
	}

	if oldIsFile {
		// If we are migrating from an old format file,
		// we require that the listed version of every
		// dependency must be installed in GOPATH, so it's
		// available to copy.
		if !eqDeps(gnew.Deps, gdisk.Deps) {
			return errors.New(strings.TrimSpace(needRestore))
		}
		gold = Godeps{}
	}

	readme := filepath.Join("vendor", "README")
	if writeFile(readme, strings.TrimSpace(Readme)+"\n"); err != nil {
		log.Println(err)
	}
	f, err := os.Create(filepath.Join("vendor", "Godeps.json"))
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := gnew.WriteTo(f); err != nil {
		return err
	}
	// We use a name starting with "_" so the go tool
	// ignores this directory when traversing packages
	// starting at the project's root. For example,
	//   godep go list ./...
	srcdir := "vendor"
	rem := subDeps(gold.Deps, gnew.Deps)
	add := subDeps(gnew.Deps, gold.Deps)
	if err := removeSrc(srcdir, rem); err != nil {
		return err
	}
	return copySrc(srcdir, add)
}

func readOldGodeps(g *Godeps) (isFile bool, err error) {
	f, err := os.Open(filepath.Join("vendor", "Godeps.json"))
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer f.Close()
	err = json.NewDecoder(f).Decode(g)
	return isFile, err
}

type revError struct {
	ImportPath string
	HaveRev    string
	WantRev    string
}

func (v *revError) Error() string {
	return v.ImportPath + ": revision is " + v.HaveRev + ", want " + v.WantRev
}

// carryVersions copies Rev and Comment from a to b for
// each dependency with an identical ImportPath. For any
// dependency in b that appears to be from the same repo
// as one in a (for example, a parent or child directory),
// the Rev must already match - otherwise it is an error.
func carryVersions(a, b *Godeps) error {
	for i := range b.Deps {
		if err := carryVersion(a, &b.Deps[i]); err != nil {
			return err
		}
	}
	return nil
}

func carryVersion(a *Godeps, db *Dependency) error {
	// First see if this exact package is already in the list.
	for _, da := range a.Deps {
		if db.ImportPath == da.ImportPath {
			db.Rev = da.Rev
			db.Comment = da.Comment
			return nil
		}
	}
	// No exact match, check for child or sibling package.
	// We can't handle mismatched versions for packages in
	// the same repo, so report that as an error.
	for _, da := range a.Deps {
		switch {
		case strings.HasPrefix(db.ImportPath, da.ImportPath+"/"):
			if da.Rev != db.Rev {
				return &revError{db.ImportPath, db.Rev, da.Rev}
			}
		case strings.HasPrefix(da.ImportPath, db.root+"/"):
			if da.Rev != db.Rev {
				return &revError{db.ImportPath, db.Rev, da.Rev}
			}
		}
	}
	// No related package in the list, must be a new repo.
	return nil
}

// subDeps returns a - b, using ImportPath for equality.
func subDeps(a, b []Dependency) (diff []Dependency) {
Diff:
	for _, da := range a {
		for _, db := range b {
			if da.ImportPath == db.ImportPath {
				continue Diff
			}
		}
		diff = append(diff, da)
	}
	return diff
}

func removeSrc(srcdir string, deps []Dependency) error {
	for _, dep := range deps {
		path := filepath.FromSlash(dep.ImportPath)
		err := os.RemoveAll(filepath.Join(srcdir, path))
		if err != nil {
			return err
		}
	}
	return nil
}

func copySrc(dir string, deps []Dependency) error {
	ok := true
	for _, dep := range deps {
		srcdir := filepath.Join(dep.ws, "src")
		rel, err := filepath.Rel(srcdir, dep.dir)
		if err != nil { // this should never happen
			return err
		}
		dstpkgroot := filepath.Join(dir, rel)
		err = os.RemoveAll(dstpkgroot)
		if err != nil {
			log.Println(err)
			ok = false
		}
		w := fs.Walk(dep.dir)
		for w.Step() {
			err = copyPkgFile(dir, srcdir, w)
			if err != nil {
				log.Println(err)
				ok = false
			}
		}
	}
	if !ok {
		return errors.New("error copying source code")
	}
	return nil
}

func copyPkgFile(dstroot, srcroot string, w *fs.Walker) error {
	if w.Err() != nil {
		return w.Err()
	}
	if c := w.Stat().Name()[0]; c == '.' || c == '_' {
		// Skip directories using a rule similar to how
		// the go tool enumerates packages.
		// See $GOROOT/src/cmd/go/main.go:/matchPackagesInFs
		w.SkipDir()
	}
	if w.Stat().IsDir() {
		return nil
	}
	rel, err := filepath.Rel(srcroot, w.Path())
	if err != nil { // this should never happen
		return err
	}
	return copyFile(filepath.Join(dstroot, rel), w.Path())
}

// copyFile copies a regular file from src to dst.
// dst is opened with os.Create.
// If the file name ends with .go,
// copyFile strips canonical import path annotations.
// These are comments of the form:
//   package foo // import "bar/foo"
//   package foo /* import "bar/foo" */
func copyFile(dst, src string) error {
	err := os.MkdirAll(filepath.Dir(dst), 0777)
	if err != nil {
		return err
	}

	linkDst, err := os.Readlink(src)
	if err == nil {
		return os.Symlink(linkDst, dst)
	}

	r, err := os.Open(src)
	if err != nil {
		return err
	}
	defer r.Close()

	w, err := os.Create(dst)
	if err != nil {
		return err
	}

	if strings.HasSuffix(dst, ".go") {
		err = copyWithoutImportComment(w, r)
	} else {
		_, err = io.Copy(w, r)
	}
	err1 := w.Close()
	if err == nil {
		err = err1
	}

	return err
}

func copyWithoutImportComment(w io.Writer, r io.Reader) error {
	b := bufio.NewReader(r)
	for {
		l, err := b.ReadBytes('\n')
		eof := err == io.EOF
		if err != nil && err != io.EOF {
			return err
		}

		// If we have data then write it out...
		if len(l) > 0 {
			// Strip off \n if it exists because stripImportComment
			_, err := w.Write(append(stripImportComment(bytes.TrimRight(l, "\n")), '\n'))
			if err != nil {
				return err
			}
		}

		if eof {
			return nil
		}
	}
}

const (
	importAnnotation = `import\s+(?:"[^"]*"|` + "`[^`]*`" + `)`
	importComment    = `(?://\s*` + importAnnotation + `\s*$|/\*\s*` + importAnnotation + `\s*\*/)`
)

var (
	importCommentRE = regexp.MustCompile(`^\s*(package\s+\w+)\s+` + importComment + `(.*)`)
	pkgPrefix       = []byte("package ")
)

// stripImportComment returns line with its import comment removed.
// If s is not a package statement containing an import comment,
// it is returned unaltered.
// FIXME: expects lines w/o a \n at the end
// See also http://golang.org/s/go14customimport.
func stripImportComment(line []byte) []byte {
	if !bytes.HasPrefix(line, pkgPrefix) {
		// Fast path; this will skip all but one line in the file.
		// This assumes there is no whitespace before the keyword.
		return line
	}
	if m := importCommentRE.FindSubmatch(line); m != nil {
		return append(m[1], m[2]...)
	}
	return line
}

// Func writeVCSIgnore writes "ignore" files inside dir for known VCSs,
// so that dir/pkg and dir/bin don't accidentally get committed.
// It logs any errors it encounters.
func writeVCSIgnore(dir string) {
	// Currently git is the only VCS for which we know how to do this.
	// Mercurial and Bazaar have similar mechasims, but they apparently
	// require writing files outside of dir.
	const ignore = "/pkg\n/bin\n"
	name := filepath.Join(dir, ".gitignore")
	err := writeFile(name, ignore)
	if err != nil {
		log.Println(err)
	}
}

// writeFile is like ioutil.WriteFile but it creates
// intermediate directories with os.MkdirAll.
func writeFile(name, body string) error {
	err := os.MkdirAll(filepath.Dir(name), 0777)
	if err != nil {
		return err
	}
	return ioutil.WriteFile(name, []byte(body), 0666)
}

const (
	Readme = `
This directory tree is generated automatically by godep.

Please do not edit.

See https://github.com/tools/godep for more information.
`
	needRestore = `
mismatched versions while migrating

It looks like you are switching from the old Godeps format
(from flag -copy=false). The old format is just a file; it
doesn't contain source code. For this migration, godep needs
the appropriate version of each dependency to be installed in
GOPATH, so that the source code is available to copy.

To fix this, run 'godep restore'.
`
)
