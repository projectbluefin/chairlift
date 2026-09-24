// ChairLift - A modern GTK4/Libadwaita system management tool
// Written in Go using puregotk bindings
package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	sgtk "github.com/frostyard/snowkit/gtk"

	"github.com/projectbluefin/chairlift/internal/app"
	"github.com/projectbluefin/chairlift/internal/firstrun"
	"github.com/projectbluefin/chairlift/internal/livery"
	"github.com/projectbluefin/chairlift/internal/version"
)

// Application version set via ldflags by goreleaser
var buildVersion = "dev"

func main() {
	processStart := time.Now()
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.Println("main: process start")

	version.Version = buildVersion

	// Rotation runs headless, from the systemd user unit the Livery page
	// installs. It must short-circuit before app.New(), which brings up GTK
	// and would otherwise try to open a display from a oneshot service.
	if len(os.Args) == 2 && os.Args[1] == livery.RotateFlag {
		ctx, cancel := livery.RotationContext()
		defer cancel()
		if err := livery.Rotate(ctx); err != nil {
			log.Printf("livery: rotation: %v", err)
			os.Exit(1)
		}
		return
	}

	application := app.New()
	defer application.Unref()
	log.Printf("main: application created in %s", time.Since(processStart))

	// SIGTERM (session logout, the E2E harness) and SIGINT quit the
	// application instead of killing the process, so Run returns, the
	// cleanup below runs, and a coverage build flushes GOCOVERDIR. The
	// handler is dropped after the first signal so a second one still
	// terminates a process whose shutdown has hung.
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTERM, os.Interrupt)
	go func() {
		sig := <-signals
		signal.Stop(signals)
		log.Printf("main: %v received, quitting", sig)
		sgtk.RunOnMainThread(application.Quit)
	}()

	code := application.Run(int32(len(os.Args)), os.Args)
	log.Printf("main: application exited with status %d", code)

	// The first-run assistant extracts its embedded SVGs into a
	// process-scoped temp directory; removing it here keeps a run from
	// leaving one behind. os.Exit below skips deferred calls, so this
	// cannot be a defer.
	if err := firstrun.CleanupAssets(); err != nil {
		log.Printf("main: %v", err)
	}

	if code > 0 {
		os.Exit(int(code))
	}
}
