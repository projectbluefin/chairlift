#!/bin/sh
# Recompile the system GSettings schema cache.
#
# ChairLift installs one schema, io.projectbluefin.chairlift.livery, which the
# Livery page reads its selections from. GSettings only sees a schema that is
# present in the compiled gschemas.compiled cache, so without this the page
# reports its settings unavailable on a freshly installed package.
set -e
if command -v glib-compile-schemas >/dev/null 2>&1; then
    glib-compile-schemas /usr/share/glib-2.0/schemas || :
fi
