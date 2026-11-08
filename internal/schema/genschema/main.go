// Command genschema writes gthok's config and hook JSON Schemas.
// It is invoked by go generate ./internal/schema and make schema.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mkusaka/gthok/internal/schema"
)

func main() {
	dir := flag.String("dir", "schema", "output directory")
	flag.Parse()
	for name, filename := range schema.Files() {
		body, err := schema.JSON(name)
		if err != nil {
			fmt.Fprintln(os.Stderr, "genschema:", err)
			os.Exit(1)
		}
		path := filepath.Join(*dir, filename)
		if err := os.WriteFile(path, body, 0o644); err != nil {
			fmt.Fprintln(os.Stderr, "genschema:", err)
			os.Exit(1)
		}
	}
}
