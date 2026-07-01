package main

import (
	"os"

	"github.com/3Craft/starSync/internal/cli"
)

func main() {
	os.Exit(cli.Execute())
}
