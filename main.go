package main

import (
	"embed"

	"github.com/sensepost/gowitness/cmd"
)

//go:embed web/dist web/static-templates
var assets embed.FS

func main() {
	cmd.Embedded = assets
	cmd.Execute()
}
