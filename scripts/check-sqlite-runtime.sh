#!/usr/bin/env bash
set -euo pipefail

# SQLite's generated code and libc are a versioned pair, not independent libraries.
sqlite_mod=$(go list -m -f '{{.GoMod}}' modernc.org/sqlite)
expected=$(awk '$1 == "modernc.org/libc" { print $2; exit }' "$sqlite_mod")
actual=$(go list -m -f '{{.Version}}' modernc.org/libc)
if [[ -z "$expected" || "$actual" != "$expected" ]]; then
  printf 'SQLite requires modernc.org/libc %s; selected %s\n' "$expected" "$actual" >&2
  exit 1
fi
printf 'SQLite runtime pairing verified: modernc.org/libc %s\n' "$actual"
