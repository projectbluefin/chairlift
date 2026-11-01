package agentmode

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"unicode"
	"unicode/utf8"

	"github.com/projectbluefin/chairlift/internal/troubleshoot"
)

// MaxPromptBytes bounds a starting prompt handed to --ask-bluefin. Report a
// Problem's prompt is a few hundred bytes; the bound keeps a stray or hostile
// file from becoming an oversized command-line argument for Goose.
const MaxPromptBytes = 4096

// PromptHeldNote follows the missing-prerequisite message when an
// --ask-bluefin prompt could not open yet, so the user knows it is kept.
const PromptHeldNote = "Your question will be waiting in Goose once it opens."

var (
	// ErrPromptEmpty is a prompt file with no text in it.
	ErrPromptEmpty = errors.New("prompt is empty")
	// ErrPromptTooLarge is a prompt file over MaxPromptBytes.
	ErrPromptTooLarge = fmt.Errorf("prompt is larger than %d bytes", MaxPromptBytes)
	// ErrPromptNotText is a prompt file that is not UTF-8 text.
	ErrPromptNotText = errors.New("prompt is not UTF-8 text")
)

// ReadPromptFile reads a starting prompt for an Ask Bluefin session. The path
// must name a regular file no larger than MaxPromptBytes; it is opened
// non-blocking so a FIFO or device cannot hang the caller, and at most one
// byte past the bound is read whatever the file claims its size is. The
// prompt is supplied text and is only ever handed to Goose as the user's
// message, never as configuration.
func ReadPromptFile(path string) (string, error) {
	if path == "" {
		return "", errors.New("no prompt file given")
	}
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return "", err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("prompt file is not a regular file")
	}
	if info.Size() > MaxPromptBytes {
		return "", ErrPromptTooLarge
	}
	data, err := io.ReadAll(io.LimitReader(f, MaxPromptBytes+1))
	if err != nil {
		return "", err
	}
	return NormalizePrompt(data)
}

// ResolvePromptPath joins a relative --prompt-file path to the working
// directory of the invocation that sent it, when it sent one.
func ResolvePromptPath(path, cwd string) string {
	if path == "" || filepath.IsAbs(path) || cwd == "" {
		return path
	}
	return filepath.Join(cwd, path)
}

// NormalizePrompt turns supplied bytes into prompt text: UTF-8 only, line
// endings folded to \n, control characters other than newline and tab
// dropped, surrounding whitespace trimmed, and the result non-empty and
// within MaxPromptBytes.
func NormalizePrompt(data []byte) (string, error) {
	if len(data) > MaxPromptBytes {
		return "", ErrPromptTooLarge
	}
	if !utf8.Valid(data) {
		return "", ErrPromptNotText
	}
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	text = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return r
		}
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, text)
	text = strings.TrimSpace(text)
	if text == "" {
		return "", ErrPromptEmpty
	}
	return text, nil
}

// pendingPrompt is the --ask-bluefin prompt waiting for the next Goose
// launch. It lives only in this process's memory, never on disk, so a prompt
// kept through the Agents setup flow leaves no trace once ChairLift exits.
var pendingPrompt struct {
	mu     sync.Mutex
	prompt string
}

// HoldPrompt keeps prompt for the next Launch, replacing any prompt already
// waiting. The next Launch that opens Goose prefills it and lets it go.
func HoldPrompt(prompt string) {
	pendingPrompt.mu.Lock()
	defer pendingPrompt.mu.Unlock()
	pendingPrompt.prompt = prompt
}

// PendingPrompt returns the prompt waiting for the next Launch, if any.
func PendingPrompt() string {
	pendingPrompt.mu.Lock()
	defer pendingPrompt.mu.Unlock()
	return pendingPrompt.prompt
}

// HeldPromptReason is the Agents page message for an --ask-bluefin whose
// prompt could not open yet: why Goose did not open, then that the prompt is
// kept for the next launch.
func HeldPromptReason(reason string) string {
	if reason == "" {
		return PromptHeldNote
	}
	return reason + " " + PromptHeldNote
}

// releasePrompt drops prompt once a launch delivered it, unless a newer
// --ask-bluefin replaced it in the meantime.
func releasePrompt(prompt string) {
	if prompt == "" {
		return
	}
	pendingPrompt.mu.Lock()
	defer pendingPrompt.mu.Unlock()
	if pendingPrompt.prompt == prompt {
		pendingPrompt.prompt = ""
	}
}

// commandForLog is a command's argv for the journal with any prompt link redacted:
// the prompt is the user's text and stays out of logs.
func commandForLog(args []string) string {
	shown := make([]string, len(args))
	for i, arg := range args {
		if strings.HasPrefix(arg, troubleshoot.NewSessionURLPrefix) {
			arg = troubleshoot.NewSessionURLPrefix + "?prompt=<redacted>"
		}
		shown[i] = arg
	}
	return strings.Join(shown, " ")
}
