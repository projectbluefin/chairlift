---
name: bounded-stream-rendering
description: Use when rendering streamed external output or opaque installation progress.
---

# Bounded progress rendering

A helper may emit unlimited output. Main-thread safety alone is insufficient:
one callback and one permanent widget per line can freeze the GUI.

- Coalesce worker output into at most one outstanding main-thread flush with
  `internal/views/progresslog.Coalescer`.
- Cap retained rows with `rowset.Tracker.TrimTo`; capping only callbacks or
  only rows leaves the other resource unbounded.
- Use `pageview.StagingLogSubtitle` to disclose truncation instead of implying
  the rolling window is a complete log. Render external text literally.
- Reuse `stageProgressSink`; keep decidable logic puregotk-free.

For opaque commands, report actual provider stages, not invented percentages or
package completion. Reuse `setupActivity` and `installProgress` for bounded stage
rows and pulsing progress. Stop timers on every outcome and disposal. Failed,
canceled, preview and live-complete outcomes stay distinct; a later failure
must not erase earlier confirmed changes.
