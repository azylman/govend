/*

Command govendor helps build packages reproducibly by fixing
their dependencies.

Example Usage

Save currently-used dependencies to file Deps:

	$ govendor

Build project using saved dependencies:

	$ GO15VENDOREXPERIMENT=1 go build

or

	$ GO15VENDOREXPERIMENT=1 go install

*/
package main
