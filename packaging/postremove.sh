#!/bin/sh
# Recompile the system GSettings schema cache after ChairLift's schema is
# removed, so the cache stops advertising a schema that is no longer present.
set -e
if command -v glib-compile-schemas >/dev/null 2>&1; then
    glib-compile-schemas /usr/share/glib-2.0/schemas || :
fi
