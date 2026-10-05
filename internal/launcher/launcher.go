// Package launcher starts desktop launch commands and reports later exit failures.
package launcher

import "os/exec"

// Start starts cmd and reports a later unsuccessful exit through reportFailure.
//
// Start returns process-start failures synchronously. reportFailure runs on the
// wait goroutine, so UI callers must marshal back to their main thread before
// touching widgets.
func Start(cmd *exec.Cmd, reportFailure func(error)) error {
	return Run(cmd, func(err error) {
		if err != nil && reportFailure != nil {
			reportFailure(err)
		}
	})
}

// Run starts cmd and calls onExit with its Wait result — nil for a clean
// exit — once it ends, so a caller can tell when a long-lived launch is
// over as well as when it failed. Start failures return synchronously and
// onExit is not called. onExit runs on the wait goroutine.
func Run(cmd *exec.Cmd, onExit func(error)) error {
	if err := cmd.Start(); err != nil {
		return err
	}

	go func() {
		err := cmd.Wait()
		if onExit != nil {
			onExit(err)
		}
	}()

	return nil
}
