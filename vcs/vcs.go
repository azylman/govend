package vcs

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"golang.org/x/tools/go/vcs"
)

type VCS struct {
	vcs *vcs.Cmd

	identifyCmd string
	describeCmd string
	diffCmd     string

	// run in sandbox repos
	existsCmd string
}

var vcsBzr = &VCS{
	vcs: vcs.ByCmd("bzr"),

	identifyCmd: "version-info --custom --template {revision_id}",
	describeCmd: "revno", // TODO(kr): find tag names if possible
	diffCmd:     "diff -r {rev}",
}

var vcsGit = &VCS{
	vcs: vcs.ByCmd("git"),

	identifyCmd: "rev-parse HEAD",
	describeCmd: "describe --tags",
	diffCmd:     "diff {rev}",

	existsCmd: "cat-file -e {rev}",
}

var vcsHg = &VCS{
	vcs: vcs.ByCmd("hg"),

	identifyCmd: "identify --id --debug",
	describeCmd: "log -r . --template {latesttag}-{latesttagdistance}",
	diffCmd:     "diff -r {rev}",

	existsCmd: "cat -r {rev} .",
}

var cmd = map[*vcs.Cmd]*VCS{
	vcsBzr.vcs: vcsBzr,
	vcsGit.vcs: vcsGit,
	vcsHg.vcs:  vcsHg,
}

func FromDir(dir, srcRoot string) (*VCS, string, error) {
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

func FromImportPath(importPath string) (*VCS, error) {
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

func (v *VCS) Identify(dir string) (string, error) {
	out, err := v.runOutput(dir, v.identifyCmd)
	return string(bytes.TrimSpace(out)), err
}

func (v *VCS) Describe(dir, rev string) string {
	out, err := v.runOutputVerboseOnly(dir, v.describeCmd, "rev", rev)
	if err != nil {
		return ""
	}
	return string(bytes.TrimSpace(out))
}

func (v *VCS) IsDirty(dir, rev string) bool {
	out, err := v.runOutput(dir, v.diffCmd, "rev", rev)
	return err != nil || len(out) != 0
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
		fmt.Fprintf(os.Stderr, "govend: missing %s command.\n", v.vcs.Name)
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
