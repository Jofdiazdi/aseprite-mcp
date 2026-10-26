package main

import (
	"flag"
	"log"
	"path/filepath"

	ase "github.com/Jofdiazdi/aseprite-mcp"
)

func main() {
	sync := flag.Bool("sync", false, "also sync the source extension in extension/agent-mcp")
	out := flag.String("out", "", "extension archive path")
	flag.Parse()
	syncDir := ""
	if *sync {
		syncDir = filepath.Join("extension", "agent-mcp")
	}
	if err := ase.PackageExtension(ase.PackagePath(*out), syncDir); err != nil {
		log.Fatal(err)
	}
}
