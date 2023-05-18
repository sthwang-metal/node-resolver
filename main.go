// package main is the entry point
package main

import (
	_ "embed"

	"go.infratographer.com/node-resolver/cmd"
)

// defaultSchema contains the default schema.graphql for infratographer only assets
//
//go:embed schema.graphql
var defaultSchema string

func main() {
	cmd.Execute(defaultSchema)
}
