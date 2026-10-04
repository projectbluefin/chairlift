---
name: yaml-key-identity
description: Use when comparing YAML scalar keys and their tags matter.
version: 1.2.0
last_updated: 2026-10-04
tags:
  - yaml
  - validation
metadata:
  type: reference
---

# Effective YAML scalar-key identity includes Tag as well as Value

**When it applies:** Walking `*yaml.Node` trees for effective merge precedence,
alias-expanded key identity or semantic deduplication. Distinguish that identity
from a source parser's explicit duplicate-key predicate; faithfully reproducing
the latter can intentionally use a different comparison.

**What to do:** A `yaml.Node`'s `.Value` is the raw string form of a scalar,
so an explicit string key `"1"` and an implicit integer key `1` both have
`Value == "1"` but are semantically distinct YAML keys (different `.Tag`,
e.g. `!!str` vs `!!int`). Any key-identity function (`mappingKeyID`-style
helpers) that hashes or compares only `Value` will silently collide these,
letting one key mask or overwrite the other during merge/dedup — a real bug,
not just a style nit. Build scalar key identity from `(Tag, Value)` together,
normalizing tags according to the pinned parser's semantics, and add a test
with same-`Value`-different-`Tag` keys (quoted `"1"` vs bare `1`) to prove they
remain distinct. `Node.Kind` alone cannot distinguish those two scalars. Reviewers will
flag a `Value`-only comparator as high severity and will keep flagging it
across revision rounds if a fix only adds a nil-guard or comment instead of
actually incorporating the tag.

## Current source boundary

ChairLift's effective merge identity is `effectiveKeyIdentity`, not the historical
`mappingKeyID` symbol. Its scalar identity includes the normalized tag and value.
The earlier `checkDuplicateMappingKeys` in `internal/config/sourcegraph.go`
intentionally reproduces yaml.v3 v3.0.1's **tag-blind** `(Kind, Value)` comparison:
two explicit scalar keys with the same value collide there even when their tags
differ. That source guard runs before effective merge processing and is not a
violation of the semantic-identity rule. Do not replace its predicate with
tag-aware deduplication merely to match this skill's title. Read
[dependency-behavior](../dependency-behavior/SKILL.md) when matching parser semantics.


**Learned from:** issue #69 phase-1 validator mill run — chunk 1's
`mappingKeyID` in `internal/config/resolve.go` compared merge/mapping keys by
`.Value` alone. The same high-severity objection ("integer 1 and string \"1\"
collide") was raised in review rounds 1, 2, and 3 without being fixed,
exhausting the review-round limit and causing the run to terminate as FAILED.
