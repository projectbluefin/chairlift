// Package launcher starts desktop launch commands and reports later exit failures.
package launcher

import "os/exec"

// Start starts cmd and reports a later unsuccessful exit through reportFailure.
//
// Start returns process-start failures synchronously. reportFailure runs on the
// wait goroutine, so UI callers must marshal back to their main thread before
// touching widgets.
func Start(cmd *exec.Cmd, reportFailure func(error)) error {
	if err := cmd.Start(); err != nil {
		return err
	}

	go func() {
		if err := cmd.Wait(); err != nil && reportFailure != nil {
			reportFailure(err)
		}
	}()

	return nil
}
