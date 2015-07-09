package main

import (
	"errors"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/azylman/govend/pkgs"
)

func update(args []string) error {
	var g Manifest
	manifest := filepath.Join(srcdir, "Deps.json")
	if err := ReadManifest(manifest, &g); err != nil {
		return err
	}
	matched := filter(args, g.Deps)
	deps, err := pkgs.LoadVCSAndUpdate(matched)
	if err != nil {
		return err
	}
	if len(deps) == 0 {
		return errors.New("no packages can be updated")
	}
	f, err := os.Create(manifest)
	if err != nil {
		return err
	}
	// Take out the old revisions, put in the new ones
	g.Deps = subDeps(g.Deps, matched)
	g.Deps = append(g.Deps, deps...)
	_, err = g.WriteTo(f)
	if err != nil {
		return err
	}
	err = f.Close()
	if err != nil {
		return err
	}
	return copySrc(filepath.FromSlash(srcdir), deps)
}

func filter(args []string, deps []pkgs.Dependency) []pkgs.Dependency {
	matched := []pkgs.Dependency{}
	for _, arg := range args {
		// Convert args into their top-level repos, e.g. D/A -> D
		arg = strings.Split(arg, "/")[0]
		found := false
		for _, dep := range deps {
			if match(arg, dep) {
				found = true
				matched = append(matched, dep)
			}
		}
		if !found {
			log.Println("not in manifest:", arg)
		}
	}
	return matched
}

func match(pat string, dep pkgs.Dependency) bool {
	return matchPattern(pat, dep.ImportPath)
}

func matchPattern(pat, name string) bool {
	re := regexp.QuoteMeta(pat)
	re = strings.Replace(re, `\.\.\.`, `.*`, -1)
	if strings.HasSuffix(re, `/.*`) {
		re = re[:len(re)-len(`/.*`)] + `(/.*)?`
	}
	return regexp.MustCompile(`^` + re).MatchString(name)
}
