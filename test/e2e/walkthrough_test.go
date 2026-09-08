package e2e

import (
	"fmt"
	"image"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/projectbluefin/chairlift/internal/navigation"
)

const walkthroughTimeout = 3 * time.Minute

// walkthroughScreen is the Xvfb screen geometry capture_walkthrough.sh uses.
// The captured PNG must match it exactly; a mismatch means the capture came
// from somewhere other than the walkthrough's own display.
const (
	walkthroughWidth  = 1400
	walkthroughHeight = 900
)

// TestWalkthroughScreenshots drives the real application through every
// navigation page under Xvfb and asserts each page rendered.
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
// windowGeometry is the captured window's position and size on the Xvfb
// root, as recorded by capture_walkthrough.sh.
type windowGeometry struct {
	X, Y, Width, Height int
}

// readWindowGeometry parses the `xdotool getwindowgeometry --shell` output
// the script wrote. A missing or unparseable file yields ok=false, and the
// caller writes the uncropped frame rather than failing: the crop is a
// presentation nicety for the docs, not an assertion.
func readWindowGeometry(t *testing.T, outDir string) (windowGeometry, bool) {
	t.Helper()

	data, err := os.ReadFile(filepath.Join(outDir, "window-geometry.env"))
	if err != nil {
		t.Logf("no window geometry recorded, writing uncropped frames: %v", err)
		return windowGeometry{}, false
	}

	fields := map[string]int{}
	for _, line := range strings.Split(string(data), "\n") {
		key, value, found := strings.Cut(strings.TrimSpace(line), "=")
		if !found {
			continue
		}
		number, err := strconv.Atoi(value)
		if err != nil {
			continue
		}
		fields[key] = number
	}

	geometry := windowGeometry{X: fields["X"], Y: fields["Y"], Width: fields["WIDTH"], Height: fields["HEIGHT"]}
	if geometry.Width <= 0 || geometry.Height <= 0 {
		t.Logf("window geometry %+v is unusable, writing uncropped frames", geometry)
		return windowGeometry{}, false
	}
	return geometry, true
}

func TestWalkthroughScreenshots(t *testing.T) {
	// The walkthrough drives the chairlift_e2e-tagged GUI, which `make e2e`
	// builds into a subdirectory of its own. The other E2E tests use the
	// untagged binaries beside it, and one of them runs `make install`,
	// which would overwrite a tagged binary sharing that path.
	app := filepath.Join(e2eBuildDir(t), "e2e", "chairlift")
	requireExecutable(t, app)

	script := filepath.Join(repoRoot(t), "test", "e2e", "capture_walkthrough.sh")
	requireExecutable(t, script)

	for _, command := range []string{"Xvfb", "xdotool", "xdpyinfo", "xwd", "dbus-run-session"} {
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

	args := append([]string{app, outDir}, names...)
	cmd := exec.Command(script, args...)
	cmd.Dir = repoRoot(t)
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("CHAIRLIFT_WALKTHROUGH_WIDTH=%d", walkthroughWidth),
		fmt.Sprintf("CHAIRLIFT_WALKTHROUGH_HEIGHT=%d", walkthroughHeight),
	)

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
		_ = cmd.Process.Kill()
		<-done
		t.Fatalf("walkthrough did not finish within %s\noutput:\n%s", walkthroughTimeout, output.String())
	}

	if !strings.Contains(output.String(), "walkthrough complete") {
		t.Fatalf("walkthrough did not report completion\noutput:\n%s", output.String())
	}

	assertBluefinGroupsRendered(t, outDir)
	assertUpdateAllRendered(t, outDir)

	geometry, cropped := readWindowGeometry(t, outDir)

	frames := make(map[string]string, len(names))
	for index, name := range names {
		path := filepath.Join(outDir, fmt.Sprintf("%d-%s.xwd", index+1, name))
		t.Run(name, func(t *testing.T) {
			// The assertions below run against the full root frame, so the
			// fixed-size and variance checks stay meaningful; only the PNG
			// written for human review is cropped to the window.
			frame := decodeFrame(t, path)
			png := frame
			if cropped {
				png = cropFrame(frame, geometry)
			}
			writePNG(t, png, strings.TrimSuffix(path, ".xwd")+".png")

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
		frames[name] = frameDigest(t, path)
	}

	// If accelerator delivery silently failed, every capture would be the
	// same page. Distinct frames are what proves navigation actually moved.
	seen := make(map[string]string, len(frames))
	for name, digest := range frames {
		if previous, duplicate := seen[digest]; duplicate {
			t.Errorf("pages %q and %q captured identical frames; the Alt+<number> accelerator did not navigate", previous, name)
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

	frame, err := decodeXWDFile(path)
	if err != nil {
		t.Fatalf("walkthrough capture %s: %v", filepath.Base(path), err)
	}
	return frame
}

// cropFrame returns the sub-image covering just the application window.
// A geometry that does not fit inside the frame yields the frame unchanged,
// since a wrong crop is worse than no crop.
func cropFrame(frame image.Image, geometry windowGeometry) image.Image {
	bounds := frame.Bounds()
	window := image.Rect(
		bounds.Min.X+geometry.X,
		bounds.Min.Y+geometry.Y,
		bounds.Min.X+geometry.X+geometry.Width,
		bounds.Min.Y+geometry.Y+geometry.Height,
	)
	if !window.In(bounds) || window.Empty() {
		return frame
	}

	type subImager interface {
		SubImage(r image.Rectangle) image.Image
	}
	if sub, ok := frame.(subImager); ok {
		return sub.SubImage(window)
	}
	return frame
}

// writePNG re-encodes a capture as a PNG beside it, so the artifacts a human
// reviews are in a format any viewer opens. A failure here is reported but
// not fatal: the assertions above already ran against the decoded frame, and
// losing the convenience copy is not a reason to fail the walkthrough.
func writePNG(t *testing.T, frame image.Image, path string) {
	t.Helper()

	file, err := os.Create(path)
	if err != nil {
		t.Logf("could not write %s: %v", filepath.Base(path), err)
		return
	}
	defer func() { _ = file.Close() }()

	if err := png.Encode(file, frame); err != nil {
		t.Logf("could not encode %s: %v", filepath.Base(path), err)
	}
}

// frameVariance returns the number of distinct colors in a frame and the
// share of pixels holding the single most common color.
func frameVariance(frame image.Image) (int, float64) {
	counts := make(map[uint64]int)
	bounds := frame.Bounds()
	total := 0

	// Every fourth pixel in each direction: enough to characterize a
	// 1400x900 frame without decoding cost dominating the test.
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

// frameDigest returns the raw capture bytes as a comparison key. Two xwd
// dumps of an unchanged screen are byte-identical, so exact equality is the
// right test for "navigation never moved".
func frameDigest(t *testing.T, path string) string {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return string(data)
}

// assertUpdateAllRendered confirms the captured session built the Update All
// group, and that its plan covers every provider available on the runner.
//
// Like the Bluefin groups, a hidden Update All group and a rendered one both
// produce a plausible Updates page, so the pixel checks cannot tell them
// apart. The group hides itself when no provider is available, which is a
// legitimate state — so this asserts the marker exists and names a non-zero
// plan, rather than asserting a fixed phase count the runner may not have.
func assertUpdateAllRendered(t *testing.T, outDir string) {
	t.Helper()

	line := findLogLine(t, outDir, "views: update all group built")
	if strings.Contains(line, "phases=0") {
		t.Errorf("Update All rendered with an empty plan; the group should have been omitted entirely\n  %s", line)
	}

	// The automatic-updates switch shares the group. capture_walkthrough.sh
	// supplies a stubbed systemd answer so the shown state is captured; the
	// hidden state is what an unstubbed runner produces and is covered by
	// internal/autoupdate's classification table.
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

	// The release channel and graphics driver moved to the System page, so
	// their own marker is what proves they rendered.
	identity := findLogLine(t, outDir, "views: image identity group built")
	for _, want := range []string{"variant=dakota", "switchable=true", "driver=standard"} {
		if !strings.Contains(identity, want) {
			t.Errorf("image identity marker missing %q\n  %s", want, identity)
		}
	}

	// The local-AI row selects its image from the stubbed GPU. The script
	// stubs an Intel + NVIDIA hybrid, so the captured frame must show the
	// CUDA stack — the case a vendor-directory catalog gets wrong.
	ai := findLogLine(t, outDir, "views: ai stack group built")
	for _, want := range []string{"vendor=nvidia", "accelerator=CUDA", "image=quay.io/ramalama/cuda:latest"} {
		if !strings.Contains(ai, want) {
			t.Errorf("ai stack marker missing %q\n  %s", want, ai)
		}
	}
}
