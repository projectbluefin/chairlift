package ubluehelper

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/projectbluefin/chairlift/internal/imageinfo"
	"github.com/projectbluefin/chairlift/internal/registrytags"
)

func TestPinInvocationGrammar(t *testing.T) {
	for _, args := range [][]string{{"pin", "20240229"}, {"pin", "20240229", "--dry-run"}, {"unpin"}, {"unpin", "--dry-run"}} {
		got, err := ParseInvocation(args)
		want := Invocation{Command: args[0], DryRun: args[len(args)-1] == "--dry-run"}
		if args[0] == "pin" {
			want.Day = "20240229"
		}
		if err != nil || got != want {
			t.Errorf("ParseInvocation(%q) = %+v, %v; want %+v", args, got, err, want)
		}
	}
	for _, args := range [][]string{
		{"pin"}, {"pin", ""}, {"pin", "20260230"}, {"pin", "20261301"}, {"pin", "20230229"},
		{"pin", "2024011"}, {"pin", "202401011"}, {"pin", "20240101\n"}, {"pin", "２０２４０１０１"},
		{"pin", "2024/101"}, {"pin", "stable"}, {"pin", "stable-20240229"}, {"pin", "ghcr.io/evil/image:stable"},
		{"pin", "sha256:abcd"}, {"pin", "--dry-run", "20240229"}, {"pin", "20240229", "stable"},
		{"pin", "20240229", "--dry-run", "--dry-run"}, {"pin", "20240229", "--force"},
		{"pin", time.Now().UTC().AddDate(0, 0, 1).Format("20060102")},
		{"unpin", "stable"}, {"unpin", "ghcr.io/evil/image"}, {"unpin", "--dry-run", "extra"}, {"unpin", "--force"},
	} {
		if got, err := ParseInvocation(args); err == nil || got != (Invocation{}) {
			t.Errorf("accepted %q: %+v, %v", args, got, err)
		}
	}
}

func TestPinDayUsesUTC(t *testing.T) {
	// Local March 1 is still February 29 in UTC.
	now := time.Date(2024, 3, 1, 0, 30, 0, 0, time.FixedZone("east", 3600))
	for _, tc := range []struct {
		day   string
		valid bool
	}{{"20240229", true}, {"20240301", false}, {"20230229", false}, {"2024022x", false}} {
		if err := ValidateDay(tc.day, now); (err == nil) != tc.valid {
			t.Errorf("ValidateDay(%q) = %v", tc.day, err)
		}
	}
	// Local February 28 is already February 29 in UTC.
	if err := ValidateDay("20240229", time.Date(2024, 2, 28, 23, 30, 0, 0, time.FixedZone("west", -3600))); err != nil {
		t.Fatal(err)
	}
}

func TestPinCandidateOrderMatchesPublishedSpellings(t *testing.T) {
	for _, tc := range []struct {
		ref, booted, day, published string
		calls                       []string
	}{
		{"ghcr.io/ublue-os/bluefin", "stable", "20260922", "stable-20260922", []string{"stable-20260922"}},
		{"ghcr.io/ublue-os/bluefin", "stable-20260922", "20260908", "stable-20260908", []string{"stable-20260908"}},
		{"ghcr.io/ublue-os/bluefin", "lts-testing.20260720", "20260714", "lts-testing-20260714", []string{"lts-testing-20260714"}},
		{"ghcr.io/projectbluefin/bluefin-lts", "testing", "20260701", "testing-20260701", []string{"testing-20260701"}},
		{"ghcr.io/projectbluefin/dakota", "latest", "20260212", "latest.20260212", []string{"latest-20260212", "latest.20260212"}},
	} {
		t.Run(tc.ref+":"+tc.booted, func(t *testing.T) {
			info := imageinfo.Info{Ref: tc.ref, Tag: "ignored", BootedRef: tc.ref + ":" + tc.booted}
			published := []string{tc.published}
			// ADR-0017 observes both spellings for these streams/days.
			if tc.published == "lts-testing-20260714" {
				published = append(published, "lts-testing.20260714")
			}
			if tc.published == "testing-20260701" {
				published = append(published, "testing.20260701")
			}
			var calls []string
			resolver := func(_ context.Context, ref, tag string) (registrytags.Tag, error) {
				if ref != tc.ref {
					t.Fatalf("resolver ref = %q", ref)
				}
				calls = append(calls, tag)
				if !slices.Contains(published, tag) {
					return registrytags.Tag{}, registrytags.ErrUnknownTag
				}
				// Registry response text must never become command text.
				return registrytags.Tag{Name: "evil/target", Digest: "evil/digest"}, nil
			}
			args, err := PinArgs(context.Background(), info, Invocation{Command: CommandPin, Day: tc.day}, resolver)
			want := []string{"switch", "--enforce-container-sigpolicy", tc.ref + ":" + tc.published}
			if err != nil || !reflect.DeepEqual(args, want) || !reflect.DeepEqual(calls, tc.calls) {
				t.Fatalf("args=%v err=%v calls=%v; want %v, %v", args, err, calls, want, tc.calls)
			}

		})
	}
}

func TestPinResolutionRefusesFailures(t *testing.T) {
	transportErr := errors.New("registry unavailable")
	for _, command := range []string{CommandPin, CommandUnpin} {
		for _, tc := range []struct {
			name    string
			failure error
			calls   int
		}{
			{"missing", registrytags.ErrUnknownTag, 2}, {"transport", transportErr, 1}, {"timeout", context.DeadlineExceeded, 1},
		} {
			t.Run(command+"/"+tc.name, func(t *testing.T) {
				calls := 0
				args, err := PinArgs(context.Background(), imageinfo.Info{Ref: "ghcr.io/ublue-os/bluefin", Tag: "stable-20240229"}, Invocation{Command: command, Day: "20240229"}, func(context.Context, string, string) (registrytags.Tag, error) {
					calls++
					return registrytags.Tag{}, tc.failure
				})
				wantCalls := tc.calls
				if command == CommandUnpin {
					wantCalls = 1
				}
				if args != nil || !errors.Is(err, tc.failure) || calls != wantCalls {
					t.Fatalf("args=%v err=%v calls=%d", args, err, calls)
				}
			})
		}
	}
}

func TestPinRejectsUnrecognizedStateBeforeResolving(t *testing.T) {
	for _, tc := range []struct{ ref, tag, command, day string }{
		{"ghcr.io/evil/image", "stable", CommandPin, "20240229"},
		{"ghcr.io/ublue-os/bluefin", "44.20260922", CommandPin, "20240229"},
		{"ghcr.io/ublue-os/bluefin", "44.20260922", CommandUnpin, ""},
		{"ghcr.io/ublue-os/bluefin", "stable", CommandUnpin, ""},
		{"ghcr.io/ublue-os/bluefin", "stable", CommandPin, "evil/image"},
		{"ghcr.io/ublue-os/bluefin", "stable", "other", ""},
		{"ghcr.io/projectbluefin/dakota-gaming", "latest", CommandPin, "20240229"},
	} {
		args, err := PinArgs(context.Background(), imageinfo.Info{Ref: tc.ref, Tag: tc.tag}, Invocation{Command: tc.command, Day: tc.day}, func(context.Context, string, string) (registrytags.Tag, error) {
			t.Fatal("unexpected lookup")
			return registrytags.Tag{}, nil
		})
		if args != nil || err == nil {
			t.Errorf("accepted %+v: %v, %v", tc, args, err)
		}
	}
}

func TestPinPreviewAndUnpinTarget(t *testing.T) {
	info := imageinfo.Info{Ref: "ghcr.io/ublue-os/bluefin", Tag: "stable", BootedRef: "ghcr.io/ublue-os/bluefin:lts-testing.20240229"}
	for _, tc := range []struct{ command, target string }{{CommandPin, "lts-testing-20240229"}, {CommandUnpin, "lts-testing"}} {
		for _, dryRun := range []bool{true, false} {
			calls := 0
			args, err := PinArgs(context.Background(), info, Invocation{Command: tc.command, Day: "20240229", DryRun: dryRun}, func(_ context.Context, ref, tag string) (registrytags.Tag, error) {
				calls++
				if tag != tc.target {
					t.Errorf("tag=%q", tag)
				}
				return registrytags.Tag{}, nil
			})
			want := []string{"switch", "--enforce-container-sigpolicy", "ghcr.io/ublue-os/bluefin:" + tc.target}
			if err != nil || !reflect.DeepEqual(args, want) || (dryRun && calls != 0) || (!dryRun && calls != 1) {
				t.Fatalf("args=%v err=%v calls=%d", args, err, calls)
			}
		}
	}
	invocation := Invocation{Command: CommandPin, Day: "20240229"}
	if args, err := PinArgs(context.Background(), info, invocation, nil); err == nil || args != nil {
		t.Fatal("nil resolver accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if args, err := PinArgs(ctx, info, invocation, func(context.Context, string, string) (registrytags.Tag, error) {
		t.Fatal("lookup after cancellation")
		return registrytags.Tag{}, nil
	}); !errors.Is(err, context.Canceled) || args != nil {
		t.Fatalf("cancellation: %v, %v", args, err)
	}
}
