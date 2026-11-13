# Working in this repo

These are the rules for any coding agent working here.

- `docs/PRD.md` is what On Air is meant to be, milestone by milestone, and the rules it must hold. `SPEC.md` is what is built so far. Keeping `SPEC.md` and the README right is part of every change.
- On Air is one Go app in two layers. The core is plain Go: it builds and tests on Linux, holds every decision, and has no cgo and no network code. The edge touches macOS through cgo and small Objective-C files. It lives under `internal/mac/`, builds only on darwin, and decides nothing: it does what the core tells it and reports what happened.
- Never import `internal/mac` from a file that builds on Linux. Every Go file in it carries `//go:build darwin`.
- A rule that `docs/PRD.md` marks as proved gets a model and a proved core. Don't weaken a rule to make a proof or a test pass. Ask instead.
- On Air writes only its own files: in its own spool directory, and in the folder it hands recordings to. It never changes or removes anything else in that folder.
- Dependencies are a person's to add. Don't change `go.mod` to bring one in.
- Commits and pull requests are authored by @gitdek alone. Don't add `Co-Authored-By` lines or any other attribution.

## Commands

- `go vet ./...` and `go test ./...`: the checks, as CI runs them.
- `tools/check-format`: fails when a Go file isn't formatted with gofmt.
