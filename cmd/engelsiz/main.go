package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"engelsiz/internal/app"
)

func main() {
	noBrowser := flag.Bool("no-browser", false, "serve without opening the default browser (CI/headless)")
	flag.Parse()
	if err := app.Run(context.Background(), app.Options{NoBrowser: *noBrowser}); err != nil {
		fmt.Fprintf(os.Stderr, "engelsiz: %v\n", err)
		os.Exit(1)
	}
}
