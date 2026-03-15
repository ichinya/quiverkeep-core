package main

import (
	"context"
	"os"

	"github.com/ichinya/quiverkeep-core/internal/cli"
)

func main() {
	if err := cli.Execute(context.Background()); err != nil {
		os.Stderr.WriteString(err.Error() + "\n")
		os.Exit(1)
	}
}
