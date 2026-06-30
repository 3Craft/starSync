package main

import (
	"os"

	"github.com/xsharp/starsync/internal/cli"
)

func main() {
	os.Exit(cli.Execute())
}
