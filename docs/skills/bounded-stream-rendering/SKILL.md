---
name: bounded-stream-rendering
description: Use when a view renders a stream of external command output into widgets.
version: 1.0.0
last_updated: 2026-09-20
tags:
  - gtk
  - performance
metadata:
  type: reference
---

# Rendering a stream one widget per line needs two caps, not one

**When it applies:** Writing or reviewing any view that turns a channel of
streamed external output — staging progress, an install log, a long-running
script's stdout — into rows, labels, or any other widget.

**The trap:** the obvious loop looks correct and is main-thread-safe, so it
passes review on the rule everyone checks:

```go
for event := range progressCh {
    evt := event
    sgtk.RunOnMainThread(func() {      // correct marshalling…
        row := adw.NewActionRow()       // …and an unbounded cost
        row.SetTitle(evt.Message)
        logExpander.AddRow(&row.Widget)
    })
}
```

The producer is a command, so the line count is whatever that command decides
to print. Every line costs one queued main-thread callback and leaves one
heavyweight widget alive for the rest of the run. A verbose helper therefore
accumulates thousands of both: the window stops responding and memory grows
without a limit. Nothing about the code says "unbounded" — the bug is in what
the *other* side of the channel is allowed to do.

**What to do — cap both halves, because either alone still fails:**

1. **Coalesce the callbacks.** Accumulate lines on the worker goroutine and
   schedule a main-thread flush only when no flush is already outstanding, so
   a burst costs one callback. `internal/views/progresslog.Coalescer.Append`
   returns exactly that "you must schedule a drain" bool.
2. **Cap the widgets.** Keep a rolling window of the most recent rows and
   evict the rest from the container — `rowset.Tracker.TrimTo(limit, remove)`.
   Capping only the pending batch bounds memory but still floods the main
   thread; coalescing only the callbacks bounds the callbacks but still leaks
   a widget per line.
3. **Say that the window is a window.** A truncated log that reads like a
   complete one sends whoever is diagnosing a failure looking for a line the
   view silently dropped, so render the retained-versus-total count
   (`pageview.StagingLogSubtitle`).
4. Put the decidable part in a puregotk-free leaf package and assert the
   wiring from there, since `internal/views` can host no test at all — see
   [gtk-headless-testing](../gtk-headless-testing/SKILL.md).

**Learned from:** issue #81 — both OS staging handlers in
`internal/views/updates_page.go` queued a callback and created a permanent
action row per output line. The fix is the `stageProgressSink` /
`internal/views/progresslog` pair, and the shape is now an `AGENTS.md`
invariant so the next streamed-output view inherits it.
