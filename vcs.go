package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"golang.org/x/tools/go/vcs"
)

type VCS struct {
	vcs *vcs.Cmd

	IdentifyCmd string
	DescribeCmd string
	DiffCmd     string

	// run in sandbox repos
	ExistsCmd string
}

var vcsBzr = &VCS{
	vcs: vcs.ByCmd("bzr"),

	IdentifyCmd: "version-info --custom --template {revision_id}",
	DescribeCmd: "revno", // TODO(kr): find tag names if possible
	DiffCmd:     "diff -r {rev}",
}

var vcsGit = &VCS{
	vcs: vcs.ByCmd("git"),

	IdentifyCmd: "rev-parse HEAD",
	DescribeCmd: "describe --tags",
	DiffCmd:     "diff {rev}",

	ExistsCmd: "cat-file -e {rev}",
}

var vcsHg = &VCS{
	vcs: vcs.ByCmd("hg"),

	IdentifyCmd: "identify --id --debug",
	DescribeCmd: "log -r . --template {latesttag}-{latesttagdistance}",
	DiffCmd:     "diff -r {rev}",

	ExistsCmd: "cat -r {rev} .",
}

var cmd = map[*vcs.Cmd]*VCS{
	vcsBzr.vcs: vcsBzr,
	vcsGit.vcs: vcsGit,
	vcsHg.vcs:  vcsHg,
}

func VCSFromDir(dir, srcRoot string) (*VCS, string, error) {
	vcscmd, reporoot, err := vcs.FromDir(dir, srcRoot)
	if err != nil {
		return nil, "", fmt.Errorf("error while inspecting %q: %v", dir, err)
	}
	vcsext := cmd[vcscmd]
	if vcsext == nil {
		return nil, "", fmt.Errorf("%s is unsupported: %s", vcscmd.Name, dir)
	}
	return vcsext, reporoot, nil
}

func VCSForImportPath(importPath string) (*VCS, error) {
	rr, err := vcs.RepoRootForImportPath(importPath, false)
	if err != nil {
		return nil, err
	}
	vcs := cmd[rr.VCS]
	if vcs == nil {
		return nil, fmt.Errorf("%s is unsupported: %s", rr.VCS.Name, importPath)
	}
	return vcs, nil
}

func (v *VCS) identify(dir string) (string, error) {
	out, err := v.runOutput(dir, v.IdentifyCmd)
	return string(bytes.TrimSpace(out)), err
}

func (v *VCS) describe(dir, rev string) string {
	out, err := v.runOutputVerboseOnly(dir, v.DescribeCmd, "rev", rev)
	if err != nil {
		return ""
	}
	return string(bytes.TrimSpace(out))
}

func (v *VCS) isDirty(dir, rev string) bool {
	out, err := v.runOutput(dir, v.DiffCmd, "rev", rev)
	return err != nil || len(out) != 0
}

func (v *VCS) exists(dir, rev string) bool {
	err := v.runVerboseOnly(dir, v.ExistsCmd, "rev", rev)
	return err == nil
}

// RevSync checks out the revision given by rev in dir.
// The dir must exist and rev must be a valid revision.
func (v *VCS) RevSync(dir, rev string) error {
	return v.run(dir, v.vcs.TagSyncCmd, "tag", rev)
}

// run runs the command line cmd in the given directory.
// keyval is a list of key, value pairs.  run expands
// instances of {key} in cmd into value, but only after
// splitting cmd into individual arguments.
// If an error occurs, run prints the command line and the
// command's combined stdout+stderr to standard error.
// Otherwise run discards the command's output.
func (v *VCS) run(dir string, cmdline string, kv ...string) error {
	_, err := v.run1(dir, cmdline, kv, true)
	return err
}

// runVerboseOnly is like run but only generates error output to standard error in verbose mode.
func (v *VCS) runVerboseOnly(dir string, cmdline string, kv ...string) error {
	_, err := v.run1(dir, cmdline, kv, false)
	return err
}

// runOutput is like run but returns the output of the command.
func (v *VCS) runOutput(dir string, cmdline string, kv ...string) ([]byte, error) {
	return v.run1(dir, cmdline, kv, true)
}

// runOutputVerboseOnly is like runOutput but only generates error output to standard error in verbose mode.
func (v *VCS) runOutputVerboseOnly(dir string, cmdline string, kv ...string) ([]byte, error) {
	return v.run1(dir, cmdline, kv, false)
}

// run1 is the generalized implementation of run and runOutput.
func (v *VCS) run1(dir string, cmdline string, kv []string, verbose bool) ([]byte, error) {
	m := make(map[string]string)
	for i := 0; i < len(kv); i += 2 {
		m[kv[i]] = kv[i+1]
	}
	args := strings.Fields(cmdline)
	for i, arg := range args {
		args[i] = expand(m, arg)
	}

	_, err := exec.LookPath(v.vcs.Cmd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "godep: missing %s command.\n", v.vcs.Name)
		return nil, err
	}

	cmd := exec.Command(v.vcs.Cmd, args...)
	cmd.Dir = dir
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err = cmd.Run()
	out := buf.Bytes()
	if err != nil {
		if verbose {
			fmt.Fprintf(os.Stderr, "# cd %s; %s %s\n", dir, v.vcs.Cmd, strings.Join(args, " "))
			os.Stderr.Write(out)
		}
		return nil, err
	}
	return out, nil
}

func expand(m map[string]string, s string) string {
	for k, v := range m {
		s = strings.Replace(s, "{"+k+"}", v, -1)
	}
	return s
}

// Mercurial has no command equivalent to git remote add.
// We handle it as a special case in process.
func hgLink(dir, remote, url string) error {
	hgdir := filepath.Join(dir, ".hg")
	if err := os.MkdirAll(hgdir, 0777); err != nil {
		return err
	}
	path := filepath.Join(hgdir, "hgrc")
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0666)
	if err != nil {
		return err
	}
	fmt.Fprintf(f, "[paths]\n%s = %s\n", remote, url)
	return f.Close()
}
