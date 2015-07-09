package pkgs

import (
	"errors"
	"log"
	"path/filepath"
	"sort"
	"strings"

	"github.com/azylman/govend/vcs"
)

const srcdir = "vendor"
const sep = "/" + srcdir + "/"

func ListDeps(name ...string) ([]Dependency, error) {
	deps := []Dependency{}
	pkgs, err := loadPacks(name...)
	if err != nil {
		return deps, err
	}
	var err1 error
	var path, seen []string
	for _, p := range pkgs {
		if p.Standard {
			log.Println("ignoring stdlib package:", p.ImportPath)
			continue
		}
		if p.Error.Err != "" {
			log.Println(p.Error.Err)
			err1 = errors.New("error loading packages")
			continue
		}
		_, reporoot, err := vcs.FromDir(p.Dir, filepath.Join(p.Root, "src"))
		if err != nil {
			log.Println(err)
			err1 = errors.New("error loading packages")
			continue
		}
		seen = append(seen, filepath.ToSlash(reporoot))
		path = append(path, p.Deps...)
	}
	var testImports []string
	for _, p := range pkgs {
		testImports = append(testImports, p.TestImports...)
		testImports = append(testImports, p.XTestImports...)
	}
	ps, err := loadPacks(testImports...)
	if err != nil {
		return deps, err
	}
	for _, p := range ps {
		if p.Standard {
			continue
		}
		if p.Error.Err != "" {
			log.Println(p.Error.Err)
			err1 = errors.New("error loading packages")
			continue
		}
		path = append(path, p.ImportPath)
		path = append(path, p.Deps...)
	}
	for i, p := range path {
		path[i] = unqualify(p)
	}
	sort.Strings(path)
	path = uniq(path)
	ps, err = loadPacks(path...)
	if err != nil {
		return deps, err
	}
	for _, pkg := range ps {
		if pkg.Error.Err != "" {
			log.Println(pkg.Error.Err)
			err1 = errors.New("error loading dependencies")
			continue
		}
		if pkg.Standard {
			continue
		}
		vcs, reporoot, err := vcs.FromDir(pkg.Dir, filepath.Join(pkg.Root, "src"))
		if err != nil {
			log.Println(err)
			err1 = errors.New("error loading dependencies")
			continue
		}
		if containsPathPrefix(seen, pkg.ImportPath) {
			continue
		}
		seen = append(seen, pkg.ImportPath)
		id, err := vcs.Identify(pkg.Dir)
		if err != nil {
			log.Println(err)
			err1 = errors.New("error loading dependencies")
			continue
		}
		if vcs.IsDirty(pkg.Dir, id) {
			log.Println("dirty working tree:", pkg.Dir)
			err1 = errors.New("error loading dependencies")
			continue
		}
		comment := vcs.Describe(pkg.Dir, id)
		deps = append(deps, Dependency{
			ImportPath: pkg.ImportPath,
			Rev:        id,
			Comment:    comment,
			Dir:        pkg.Dir,
			Workspace:  pkg.Root,
			Root:       filepath.ToSlash(reporoot),
			vcs:        vcs,
		})
	}
	return deps, err1
}

// A Dependency is a specific revision of a package.
type Dependency struct {
	ImportPath string
	Comment    string `json:",omitempty"` // Description of commit, if present.
	Rev        string // VCS-specific commit ID.

	// used by command save & update
	Workspace string `json:"-"` // workspace
	Root      string `json:"-"` // import path to repo root
	Dir       string `json:"-"` // full path to package

	// used by command update
	pkg *pack

	// used by command go
	vcs *vcs.VCS
}

// containsPathPrefix returns whether any string in a
// is s or a directory containing s.
// For example, pattern ["a"] matches "a" and "a/b"
// (but not "ab").
func containsPathPrefix(pats []string, s string) bool {
	for _, pat := range pats {
		if pat == s || strings.HasPrefix(s, pat+"/") {
			return true
		}
	}
	return false
}

func uniq(a []string) []string {
	i := 0
	s := ""
	for _, t := range a {
		if t != s {
			a[i] = t
			i++
			s = t
		}
	}
	return a[:i]
}

// unqualify returns the part of importPath after the last
// occurrence of the signature path elements
// (vendor) that always precede imported
// packages in rewritten import paths.
//
// For example,
//   unqualify(C)                         = C
//   unqualify(D/vendor/C) = C
func unqualify(importPath string) string {
	if i := strings.LastIndex(importPath, sep); i != -1 {
		importPath = importPath[i+len(sep):]
	}
	return importPath
}

func LoadVCSAndUpdate(deps []Dependency) ([]Dependency, error) {
	var err1 error
	var paths []string
	for _, dep := range deps {
		paths = append(paths, dep.ImportPath)
	}
	ps, err := loadPacks(paths...)
	if err != nil {
		return nil, err
	}
	noupdate := make(map[string]bool) // repo roots
	var candidates []Dependency
	var tocopy []Dependency
	for i := range deps {
		dep := deps[i]
		for _, pkg := range ps {
			if dep.ImportPath == pkg.ImportPath {
				dep.pkg = pkg
				break
			}
		}
		if dep.pkg == nil {
			log.Println(dep.ImportPath + ": error listing package")
			err1 = errors.New("error loading dependencies")
			continue
		}
		if dep.pkg.Error.Err != "" {
			log.Println(dep.pkg.Error.Err)
			err1 = errors.New("error loading dependencies")
			continue
		}
		vcs, reporoot, err := vcs.FromDir(dep.pkg.Dir, filepath.Join(dep.pkg.Root, "src"))
		if err != nil {
			log.Println(err)
			err1 = errors.New("error loading dependencies")
			continue
		}
		dep.Dir = dep.pkg.Dir
		dep.Workspace = dep.pkg.Root
		dep.Root = filepath.ToSlash(reporoot)
		dep.vcs = vcs
		candidates = append(candidates, dep)
	}
	if err1 != nil {
		return nil, err1
	}

	for _, dep := range candidates {
		dep.Dir = dep.pkg.Dir
		dep.Workspace = dep.pkg.Root
		if noupdate[dep.Root] {
			continue
		}
		id, err := dep.vcs.Identify(dep.pkg.Dir)
		if err != nil {
			log.Println(err)
			err1 = errors.New("error loading dependencies")
			continue
		}
		if dep.vcs.IsDirty(dep.pkg.Dir, id) {
			log.Println("dirty working tree:", dep.pkg.Dir)
			err1 = errors.New("error loading dependencies")
			break
		}
		dep.Rev = id
		dep.Comment = dep.vcs.Describe(dep.pkg.Dir, id)
		tocopy = append(tocopy, dep)
	}
	if err1 != nil {
		return nil, err1
	}
	return tocopy, nil
}
