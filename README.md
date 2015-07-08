### Govendor

Command govendor helps build packages reproducibly by fixing their dependencies.

This tool assumes you are working in a standard Go workspace,
as described in http://golang.org/doc/code.html. We require Go 1.5
or newer to build govendor itself, and you can only use it with go 1.5 and newer.

**NOTE**: This is currently a forked, heavily stripped down version of https://github.com/tools/godep.

### Install

	$ go get github.com/azylman/govendor

#### Getting Started

How to add govendor in a new project.

Assuming you've got everything working already, so you can
build your project with `go install` and test it with `go test`,
it's one command to start using:

	$ govendor save

This will save a list of dependencies to the file vendor/Godeps.json (for future compatibility with godep),
and copy their source code into vendor/.
Read over its contents and make sure it looks reasonable.
Then commit the whole vendor directory to version control.

#### Add a Dependency

To add a new package foo/bar, do this:

1. Run `go get foo/bar`
2. Edit your code to import foo/bar.
3. Run `govendor save`.

#### Update a Dependency

To update a package from your `$GOPATH`, do this:

1. Run `go get -u foo/bar`
2. Run `govendor update foo/bar`. (You can use the `...` wildcard,
for example `govendor update foo/...`).

Before committing the change, you'll probably want to inspect
the changes to Godeps, for example with `git diff`,
and make sure it looks reasonable.

### File Format

Godeps is a json file with the following structure:

```go
type Godeps struct {
	ImportPath string
	GoVersion  string   // Abridged output of 'go version'.
	Deps       []struct {
		ImportPath string
		Comment    string // Description of commit, if present.
		Rev        string // VCS-specific commit ID.
	}
}
```

Example Godeps:

```json
{
	"ImportPath": "github.com/kr/hk",
	"GoVersion": "go1.1.2",
	"Deps": [
		{
			"ImportPath": "code.google.com/p/go-netrc/netrc",
			"Rev": "28676070ab99"
		},
		{
			"ImportPath": "github.com/kr/binarydist",
			"Rev": "3380ade90f8b0dfa3e363fd7d7e941fa857d0d13"
		}
	]
}
```
