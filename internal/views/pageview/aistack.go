package pageview

import (
	"fmt"
	"strings"
)

// AIStackGroupTitle is the Agents page's local-AI group heading.
func AIStackGroupTitle() string {
	return "Local AI"
}

// AIStackGroupDescription states the property that makes the feature worth
// having at all. It is the group description rather than the row subtitle
// because the subtitle has to carry the two facts that vary per machine —
// graphics acceleration and download size — and a subtitle carrying all
// three reads as a paragraph.
func AIStackGroupDescription() string {
	return "Answers are generated on this computer. Nothing you type is sent to a cloud service."
}

// AIStackRow returns the local-AI switch row's text. hardware is the
// human-readable graphics vendor ("AMD", "NVIDIA", "Intel"), and accelerated
// is false when no graphics hardware was found. The compute stack's own name
// is deliberately absent: "ROCm" tells a person nothing about whether the
// feature will work for them, and it is available in the Details row for
// anyone it does mean something to.
func AIStackRow(hardware string, accelerated bool) Row {
	row := Row{Title: "Run an AI model on this computer"}
	if accelerated {
		row.Subtitle = fmt.Sprintf("Your %s graphics will speed it up. Turning this on downloads several gigabytes.", hardware)
		return row
	}
	// A machine with no graphics card still gets a working model, but saying
	// so without saying it is slow would set the wrong expectation for a
	// first run that takes minutes per answer.
	row.Subtitle = "No graphics card was found, so answers will be slow. Turning this on downloads several gigabytes."
	return row
}

// AIStackWorkingSubtitle returns the subtitle shown while the switch is
// acting.
func AIStackWorkingSubtitle(enabling bool) string {
	if enabling {
		return "Starting…"
	}
	return "Stopping…"
}

// AIStackResultSubtitle returns the subtitle after the switch has acted.
// The address the model answers on lives in the Details row, so this stays a
// sentence rather than becoming a key/value dump.
func AIStackResultSubtitle(enabled bool) string {
	if enabled {
		return "Running. The model downloads in the background the first time, which can take a while."
	}
	return "Stopped. The model that was already downloaded was kept."
}

// AIStackFailureSubtitle returns the subtitle when the switch could not do
// what was asked. Turning the feature off can fail in a way that leaves the
// model running — the service refused to stop and a follow-up check could
// not prove otherwise — and in that case nothing was removed, so the text
// must not suggest the feature is off.
func AIStackFailureSubtitle(enabling bool) string {
	if enabling {
		return "Could not start. Nothing on this computer was changed."
	}
	return "Still running — it could not be stopped, so nothing was removed."
}

// AIStackFailureToast returns the toast for the same two failures. The
// underlying error is logged rather than shown: it names the background
// service by its file name, which is not something to put in front of a
// person.
func AIStackFailureToast(enabling bool) string {
	if enabling {
		return "Local AI could not start. Nothing was changed."
	}
	return "Local AI is still running. It could not be stopped, so nothing was removed."
}

// AIStackDetailsTitle is the expander holding the technical identity a
// person needs only when pointing another application at the model.
func AIStackDetailsTitle() string {
	return "Details"
}

// AIStackFacts is everything the Details rows are derived from.
type AIStackFacts struct {
	// Model is the model reference the service is configured to serve.
	Model string
	// Hardware is the human-readable graphics vendor name.
	Hardware string
	// Accelerator is the compute stack selected for that hardware.
	Accelerator string
	// Accelerated is false when no graphics hardware was found.
	Accelerated bool
	// Port is the port the model answers on.
	Port int
}

// AIStackDetails returns the rows behind the Details expander.
func AIStackDetails(f AIStackFacts) []Row {
	return []Row{
		{Title: "Model", Subtitle: AIModelName(f.Model)},
		{Title: "Graphics acceleration", Subtitle: aiAccelerationDetail(f)},
		// Without the address a person has a running model and no way to
		// reach it from anything.
		{Title: "Address for other apps", Subtitle: fmt.Sprintf("localhost:%d", f.Port)},
	}
}

func aiAccelerationDetail(f AIStackFacts) string {
	if !f.Accelerated {
		return "None — runs on the processor"
	}
	// "Intel (Intel oneAPI)" reads as a stutter, so a stack that already
	// names its vendor is shown on its own.
	if strings.Contains(f.Accelerator, f.Hardware) {
		return f.Accelerator
	}
	return fmt.Sprintf("%s (%s)", f.Hardware, f.Accelerator)
}

// AIModelName turns a model reference into something readable. References
// carry a source scheme ("ollama://llama3.2:3b") that identifies where the
// weights come from, which is noise to everyone except the person who
// configured it.
func AIModelName(ref string) string {
	name := strings.TrimSpace(ref)
	if name == "" {
		return "Not configured"
	}
	if _, rest, found := strings.Cut(name, "://"); found {
		if rest = strings.TrimSpace(rest); rest != "" {
			return rest
		}
	}
	return name
}
