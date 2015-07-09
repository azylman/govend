package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/azylman/govend/pkgs"
	"github.com/kr/fs"
)

func save(args []string) error {
	if len(args) == 0 {
		args = []string{"./..."}
	}
	ver, err := goVersion()
	if err != nil {
		return err
	}

	manifest, err := readCurManifest()
	if err != nil {
		return err
	}
	path, err := pkgs.ImportPath(".")
	if err != nil {
		return err
	}
	manifest.ImportPath = path
	manifest.GoVersion = ver

	deps, err := pkgs.ListDeps(args...)
	if err != nil {
		return err
	}

	rem := subDeps(manifest.Deps, deps)
	add := subDeps(deps, manifest.Deps)
	manifest.Deps = subDeps(manifest.Deps, rem)
	manifest.Deps = append(manifest.Deps, add...)
	if err := checkForConflicts(manifest.Deps); err != nil {
		return err
	}

	readme := filepath.Join(srcdir, "README")
	if writeFile(readme, strings.TrimSpace(Readme)+"\n"); err != nil {
		log.Println(err)
	}
	f, err := os.Create(filepath.Join(srcdir, "Deps.json"))
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := manifest.WriteTo(f); err != nil {
		return err
	}

	if err := removeSrc(srcdir, rem); err != nil {
		return err
	}
	return copySrc(srcdir, add)
}

func checkForConflicts(deps []pkgs.Dependency) error {
	// We can't handle mismatched versions for packages in
	// the same repo, so report that as an error.
	for _, da := range deps {
		for _, db := range deps {
			switch {
			case strings.HasPrefix(db.ImportPath, da.ImportPath+"/"):
				if da.Rev != db.Rev {
					return fmt.Errorf("conflicting revisions %s and %s", da.Rev, db.Rev)
				}
			case strings.HasPrefix(da.ImportPath, db.Root+"/"):
				if da.Rev != db.Rev {
					return fmt.Errorf("conflicting revisions %s and %s", da.Rev, db.Rev)
				}
			}
		}
	}
	return nil
}

func readCurManifest() (Manifest, error) {
	f, err := os.Open(filepath.Join(srcdir, "Deps.json"))
	if os.IsNotExist(err) {
		return Manifest{}, nil
	}
	if err != nil {
		return Manifest{}, err
	}
	defer f.Close()
	var man Manifest
	err = json.NewDecoder(f).Decode(&man)
	if man.Deps == nil {
		man.Deps = []pkgs.Dependency{}
	}
	return man, err
}

// subDeps returns a - b, using ImportPath for equality.
func subDeps(a, b []pkgs.Dependency) (diff []pkgs.Dependency) {
	diff = []pkgs.Dependency{}
	for _, da := range a {
		dupe := false
		for _, db := range b {
			if da.ImportPath == db.ImportPath {
				dupe = true
				break
			}
		}
		if !dupe {
			diff = append(diff, da)
		}
	}
	return diff
}

func removeSrc(srcdir string, deps []pkgs.Dependency) error {
	for _, dep := range deps {
		path := filepath.FromSlash(dep.ImportPath)
		err := os.RemoveAll(filepath.Join(srcdir, path))
		if err != nil {
			return err
		}
	}
	return nil
}

func copySrc(dir string, deps []pkgs.Dependency) error {
	ok := true
	for _, dep := range deps {
		srcdir := filepath.Join(dep.Workspace, "src")
		rel, err := filepath.Rel(srcdir, dep.Dir)
		if err != nil { // this should never happen
			return err
		}
		dstpkgroot := filepath.Join(dir, rel)
		err = os.RemoveAll(dstpkgroot)
		if err != nil {
			log.Println(err)
			ok = false
		}
		w := fs.Walk(dep.Dir)
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
This directory tree is generated automatically by govend.

Please do not edit.

See https://github.com/azylman/govend for more information.
`
)

// goVersion returns the version string of the Go compiler
// currently installed, e.g. "go1.1rc3".
func goVersion() (string, error) {
	// govend might have been compiled with a different
	// version, so we can't just use runtime.Version here.
	cmd := exec.Command("go", "version")
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	p := strings.Split(string(out), " ")
	if len(p) < 3 {
		return "", fmt.Errorf("Error splitting output of `go version`: Expected 3 or more elements, but there are < 3: %q", string(out))
	}
	return p[2], nil
}
