package e2e

import (
	"crypto/sha256"
	"fmt"
	"image"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/projectbluefin/chairlift/internal/navigation"
	"github.com/projectbluefin/chairlift/internal/printerapp"
)

const walkthroughTimeout = 3 * time.Minute

// The headless monitor capture_walkthrough.sh runs under is exactly the
// main window's default size (internal/window: SetDefaultSize(900, 700)), so
// Mutter fits the window to it and every frame is the window, uncropped. A
// frame of any other size came from somewhere other than this session.
const (
	walkthroughWidth  = 900
	walkthroughHeight = 700
)

// TestWalkthroughScreenshots drives the real application through every
// navigation page on a headless native Wayland session and asserts each page
// rendered.
//
// What this verifies, precisely: the application launched, the window
// appeared, every advertised Alt+<number> accelerator navigated somewhere,
// each page painted something other than a blank frame, no two pages
// rendered identically, and the process survived the whole sequence without
// crashing. What it does not and cannot verify is that any page looks
// *correct* — the images are artifacts for a human to review, and the
// assertions below are a floor, not a judgement of the UI.
//
// The whole run is in --dry-run, so none of the three Bluefin-family
// toggles can execute a real mutation while the screenshots are taken.
func TestWalkthroughScreenshots(t *testing.T) {
	// The walkthrough drives the chairlift_e2e-tagged GUI, which `make e2e`
	// builds into a subdirectory of its own. The other E2E tests use the
	// untagged binaries beside it, and one of them runs `make install`,
	// which would overwrite a tagged binary sharing that path.
	app := filepath.Join(e2eBuildDir(t), "e2e", "chairlift")
	requireExecutable(t, app)

	script := filepath.Join(repoRoot(t), "test", "e2e", "capture_walkthrough.sh")
	requireExecutable(t, script)

	session := filepath.Join(repoRoot(t), "test", "e2e", "wayland_session.sh")
	requireExecutable(t, session)

	for _, command := range []string{"dbus-run-session", "mutter", "pipewire", "wireplumber", "gst-launch-1.0"} {
		requireCommand(t, command)
	}

	// Page names and their order come from internal/navigation, the single
	// authority for the accelerators the script presses. A page added there
	// is screenshotted here without touching this test.
	items := navigation.Items()
	if len(items) == 0 {
		t.Fatal("navigation.Items() is empty; there is nothing to walk through")
	}
	names := make([]string, 0, len(items))
	for _, item := range items {
		names = append(names, item.Name)
	}

	outDir := walkthroughOutputDir(t)

	runtimeDir := filepath.Join(outDir, "runtime")
	if err := os.MkdirAll(runtimeDir, 0o700); err != nil {
		t.Fatalf("create isolated XDG_RUNTIME_DIR: %v", err)
	}

	args := append([]string{"--", session, script, app, outDir}, names...)
	cmd := exec.Command("dbus-run-session", args...)
	cmd.Dir = repoRoot(t)
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("CHAIRLIFT_WAYLAND_SIZE=%dx%d", walkthroughWidth, walkthroughHeight),
		"XDG_RUNTIME_DIR="+runtimeDir,
	)
	// A private session, so a timeout can stop the compositor, PipeWire and
	// the application along with the script.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	output := &lockedBuffer{}
	cmd.Stdout = output
	cmd.Stderr = output

	if err := cmd.Start(); err != nil {
		t.Fatalf("start walkthrough: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("walkthrough failed: %v\noutput:\n%s", err, output.String())
		}
	case <-time.After(walkthroughTimeout):
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		<-done
		t.Fatalf("walkthrough did not finish within %s\noutput:\n%s", walkthroughTimeout, output.String())
	}

	if !strings.Contains(output.String(), "walkthrough complete") {
		t.Fatalf("walkthrough did not report completion\noutput:\n%s", output.String())
	}

	assertBluefinGroupsRendered(t, outDir)
	assertAutomaticUpdatesRendered(t, outDir)
	// The reset group is opt-in and the capture enables it explicitly; a
	// higher-priority checkout config must not silently hide both rows.
	findLogLine(t, outDir, "views: reset group built")

	// The setup assistant is captured first, before any page: the script
	// launches with --setup, so the frame shows the welcome screen over the
	// Updates page. It is checked like a page and must differ from every
	// page frame, which proves the dialog was actually on screen.
	captures := append([]string{"setup"}, names...)
	frames := make(map[string]string, len(captures))
	for index, name := range captures {
		path := filepath.Join(outDir, fmt.Sprintf("%d-%s.png", index, name))
		t.Run(name, func(t *testing.T) {
			frame := decodeFrame(t, path)

			bounds := frame.Bounds()
			if bounds.Dx() != walkthroughWidth || bounds.Dy() != walkthroughHeight {
				t.Errorf("%s is %dx%d, want %dx%d",
					filepath.Base(path), bounds.Dx(), bounds.Dy(), walkthroughWidth, walkthroughHeight)
			}

			// A crashed or never-painted window captures as one flat color.
			// Requiring both a non-trivial palette and a non-dominant modal
			// color rejects a blank frame and a frame that is a solid
			// background with a stray artifact. The floor is deliberately
			// low: a legitimately sparse page — Help is a short list of
			// links on a flat background — samples to well under two
			// hundred colors, so a high threshold would fail correct
			// renders. Combined with the identical-frame check below, this
			// is enough to catch "nothing painted".
			distinct, modalShare := frameVariance(frame)
			if distinct < 40 {
				t.Errorf("%s has only %d distinct colors, want at least 40 — the page appears not to have rendered",
					filepath.Base(path), distinct)
			}
			if modalShare > 0.98 {
				t.Errorf("%s is %.1f%% a single color, want under 98%% — the page appears blank",
					filepath.Base(path), modalShare*100)
			}
		})
		frames[name] = frameDigest(decodeFrame(t, path))
	}

	// If accelerator delivery silently failed, every capture would be the
	// same page. Distinct frames are what proves navigation actually moved;
	// the setup frame differing from the Updates frame proves the assistant
	// was on screen and Escape dismissed it.
	seen := make(map[string]string, len(frames))
	for name, digest := range frames {
		if previous, duplicate := seen[digest]; duplicate {
			t.Errorf("captures %q and %q are identical frames; the Alt+<number> accelerator did not navigate, or the setup assistant never presented", previous, name)
			continue
		}
		seen[digest] = name
	}

	t.Logf("walkthrough screenshots written to %s", outDir)
}

// walkthroughOutputDir returns where screenshots are written. It defaults to
// a temporary directory that Go removes with the test, and honors
// CHAIRLIFT_WALKTHROUGH_DIR so a developer (or a CI artifact-upload step)
// can keep the images for review.
func walkthroughOutputDir(t *testing.T) string {
	t.Helper()

	dir := os.Getenv("CHAIRLIFT_WALKTHROUGH_DIR")
	if dir == "" {
		return t.TempDir()
	}
	absolute, err := filepath.Abs(dir)
	if err != nil {
		t.Fatalf("resolve CHAIRLIFT_WALKTHROUGH_DIR %q: %v", dir, err)
	}
	if err := os.MkdirAll(absolute, 0o755); err != nil {
		t.Fatalf("create walkthrough output dir %s: %v", absolute, err)
	}
	return absolute
}

func decodeFrame(t *testing.T, path string) image.Image {
	t.Helper()

	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("walkthrough capture %s: %v", filepath.Base(path), err)
	}
	defer func() { _ = file.Close() }()
	frame, err := png.Decode(file)
	if err != nil {
		t.Fatalf("walkthrough capture %s: %v", filepath.Base(path), err)
	}
	return frame
}

// frameVariance returns the number of distinct colors in a frame and the
// share of pixels holding the single most common color.
func frameVariance(frame image.Image) (int, float64) {
	counts := make(map[uint64]int)
	bounds := frame.Bounds()
	total := 0

	// Every fourth pixel in each direction: enough to characterize a frame
	// without decoding cost dominating the test.
	for y := bounds.Min.Y; y < bounds.Max.Y; y += 4 {
		for x := bounds.Min.X; x < bounds.Max.X; x += 4 {
			r, g, b, a := frame.At(x, y).RGBA()
			key := uint64(r)<<48 | uint64(g)<<32 | uint64(b)<<16 | uint64(a)
			counts[key]++
			total++
		}
	}

	modal := 0
	for _, count := range counts {
		if count > modal {
			modal = count
		}
	}
	if total == 0 {
		return 0, 1
	}
	return len(counts), float64(modal) / float64(total)
}

// frameDigest hashes a frame's decoded pixels. Two captures of an unchanged
// screen hold identical pixels, so exact equality is the right test for
// "navigation never moved", independent of how the encoder compressed them.
func frameDigest(frame image.Image) string {
	hash := sha256.New()
	bounds := frame.Bounds()
	pixel := make([]byte, 8)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r, g, b, a := frame.At(x, y).RGBA()
			pixel[0], pixel[1] = byte(r>>8), byte(r)
			pixel[2], pixel[3] = byte(g>>8), byte(g)
			pixel[4], pixel[5] = byte(b>>8), byte(b)
			pixel[6], pixel[7] = byte(a>>8), byte(a)
			hash.Write(pixel)
		}
	}
	return string(hash.Sum(nil))
}

// assertAutomaticUpdatesRendered confirms the captured session built the
// automatic-background-updates switch in its on state.
//
// Like the Bluefin groups, a hidden switch and a rendered one both produce a
// plausible Updates page, so the pixel checks cannot tell them apart. The
// group hides itself on a host with no unattended-update timer, which is a
// legitimate state and what an unstubbed runner produces —
// capture_walkthrough.sh supplies a stubbed systemd answer so the shown
// state is the one captured, and internal/autoupdate's classification table
// covers the hidden one.
//
// There is deliberately no assertion here about the page's update action.
// It used to check a "views: update all group built" marker emitted by the
// legacy Update All group; that group is gone, and the status-first shell
// that replaced it renders from an immutable updateflow.Snapshot whose
// every phase and action is already covered by internal/updateflow and
// internal/views/updatepresent table tests. Re-deriving that here would pin
// a log line rather than a behaviour.
func assertAutomaticUpdatesRendered(t *testing.T, outDir string) {
	t.Helper()

	autoLine := findLogLine(t, outDir, "views: automatic updates row built")
	if !strings.Contains(autoLine, "state=on") {
		t.Errorf("automatic updates row did not render in the on state\n  %s", autoLine)
	}
}

// findLogLine returns the first line of the walkthrough's application log
// containing marker, failing the test when it is absent.
func findLogLine(t *testing.T, outDir, marker string) string {
	t.Helper()

	path := filepath.Join(outDir, "chairlift.log")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading walkthrough application log: %v", err)
	}

	index := strings.Index(string(data), marker)
	if index < 0 {
		t.Fatalf("the captured session did not log %q\nlog:\n%s", marker, data)
	}
	line := string(data)[index:]
	if end := strings.IndexByte(line, '\n'); end >= 0 {
		line = line[:end]
	}
	return line
}

// assertBluefinGroupsRendered confirms the captured session actually built
// the release-channel, developer-mode, and gaming rows.
//
// The pixel checks above cannot tell a Features page carrying those three
// groups from one where all three hid themselves — both render as a
// plausible page. Without this the walkthrough would pass on a runner where
// the feature was entirely absent, which is precisely the runner it executes
// on: a GitHub runner is not a Bluefin system. capture_walkthrough.sh
// supplies a Dakota image descriptor through the dry-run-only override for
// exactly this reason.
func assertBluefinGroupsRendered(t *testing.T, outDir string) {
	t.Helper()

	line := findLogLine(t, outDir, "views: bluefin groups built")

	// The descriptor the script supplies is Dakota on its stable stream:
	// every row visible, and the channel switch actually switchable.
	for _, want := range []string{
		"variant=dakota",
		"tag=latest",
		"channel=stable",
		"switchable=true",
		"dx_group=true",
		"gaming_group=true",
	} {
		if !strings.Contains(line, want) {
			t.Errorf("Bluefin group marker missing %q\n  %s", want, line)
		}
	}

	// The release channel and graphics driver live on the Updates page,
	// beside the thing that changes them, so their own marker is what
	// proves they rendered. (They were on the System page until it was
	// deleted — "about this computer" is GNOME Settings' job.)
	identity := findLogLine(t, outDir, "views: image identity group built")
	for _, want := range []string{"variant=dakota", "switchable=true", "driver=standard"} {
		if !strings.Contains(identity, want) {
			t.Errorf("image identity marker missing %q\n  %s", want, identity)
		}
	}

	// Agent Mode is llmman on loopback. The marker is the Agents page's own,
	// and it names the fixed address so a bind drifting off loopback fails
	// here as well as in internal/aistack's unit test.
	ai := findLogLine(t, outDir, "views: agents page built")
	for _, want := range []string{"runtime=llmman", "address=127.0.0.1:17434", "state="} {
		if !strings.Contains(ai, want) {
			t.Errorf("agents page marker missing %q\n  %s", want, ai)
		}
	}

	// The Printers group is floored on Podman, which capture_walkthrough.sh
	// supplies through CHAIRLIFT_CAPABILITIES. Every family is blocked until
	// its image accepts an administration credential (ADR-0016), and the
	// marker says so: a screenshot of the Features page must show one locked
	// row per family, not a page where the group silently hid itself. The
	// count comes from the families table so adding a family does not touch
	// this gate.
	printers := findLogLine(t, outDir, "views: printers group built")
	families := len(printerapp.Families())
	if want := fmt.Sprintf("families=%d blocked=%d", families, families); !strings.Contains(printers, want) {
		t.Errorf("printers group marker missing %q\n  %s", want, printers)
	}
}
