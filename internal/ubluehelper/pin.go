package ubluehelper

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/projectbluefin/chairlift/internal/imageinfo"
	"github.com/projectbluefin/chairlift/internal/registrytags"
)

// ValidateDay accepts only eight ASCII digits naming a real calendar day no
// later than today in UTC. Both sides of the privilege boundary use it.
func ValidateDay(day string, now time.Time) error {
	if len(day) != 8 {
		return fmt.Errorf("day must contain exactly eight ASCII digits")
	}
	for i := range len(day) {
		if day[i] < '0' || day[i] > '9' {
			return fmt.Errorf("day must contain exactly eight ASCII digits")
		}
	}
	date, err := time.Parse("20060102", day)
	if err != nil {
		return fmt.Errorf("invalid calendar day: %w", err)
	}
	if date.After(now.UTC()) {
		return fmt.Errorf("day is later than today in UTC")
	}
	return nil
}

// TagResolver checks a single locally constructed tag. Returned registry data
// is deliberately discarded; it can never supply the switch target.
type TagResolver func(context.Context, string, string) (registrytags.Tag, error)

// PinArgs derives and verifies the complete bootc argv for pin or unpin.
// Only a missing tag permits trying the next spelling; all other errors refuse
// immediately. Dry runs preview the first candidate without a registry read.
func PinArgs(ctx context.Context, info imageinfo.Info, invocation Invocation, resolve TagResolver) ([]string, error) {
	stream := info.EffectiveTag()
	build, pinned := registrytags.ParseBuild(stream)
	if pinned {
		stream = build.Stream
	}
	if !imageinfo.KnownStream(info.CleanRef(), stream) {
		return nil, fmt.Errorf("pin target: stream %q is not defined for %s", stream, info.CleanRef())
	}
	var tags []string
	switch invocation.Command {
	case CommandPin:
		if err := ValidateDay(invocation.Day, time.Now()); err != nil {
			return nil, err
		}
		tags = []string{stream + "-" + invocation.Day, stream + "." + invocation.Day}
	case CommandUnpin:
		if !pinned {
			return nil, fmt.Errorf("pin target: running tag is not a dated build")
		}
		tags = []string{stream}
	default:
		return nil, fmt.Errorf("unsupported pin command %q", invocation.Command)
	}
	for _, tag := range tags {
		target := info.CleanRef() + ":" + tag
		if !invocation.DryRun {
			if resolve == nil {
				return nil, fmt.Errorf("pin target: no registry resolver")
			}
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if _, err := resolve(ctx, info.CleanRef(), tag); err != nil {
				if errors.Is(err, registrytags.ErrUnknownTag) {
					continue
				}
				return nil, fmt.Errorf("verifying %s: %w", target, err)
			}
		}
		return []string{"switch", "--enforce-container-sigpolicy", target}, nil
	}
	return nil, fmt.Errorf("pin target: no published build for %s among %v: %w", info.CleanRef(), tags, registrytags.ErrUnknownTag)
}
