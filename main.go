package main

import (
	"context"
	"os"

	"rocmplete/internal/cli"
)

func main() {
	os.Exit(cli.Main(context.Background(), os.Args[1:], os.Stdout, os.Stderr))
}
