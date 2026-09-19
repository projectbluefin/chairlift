// ChairLift - A modern GTK4/Libadwaita system management tool
// Written in Go using puregotk bindings
package main

import (
	"log"
	"os"
	"time"

	"github.com/projectbluefin/chairlift/internal/app"
	"github.com/projectbluefin/chairlift/internal/version"
)

// Application version set via ldflags by goreleaser
var buildVersion = "dev"

func main() {
	processStart := time.Now()
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.Println("main: process start")

	version.Version = buildVersion

	application := app.New()
	defer application.Unref()
	log.Printf("main: application created in %s", time.Since(processStart))

	if code := application.Run(int32(len(os.Args)), os.Args); code > 0 {
		os.Exit(int(code))
	}
}
