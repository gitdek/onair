# On Air

A menu-bar recorder for macOS that never loses a meeting. It keeps a recording playable on disk from its first seconds, so nothing is left to fail at Stop, and hands each finished recording to On the Record. It is the live side of On the Record: later milestones show what a conversation connects to while it is happening.

**Status:** founding. Nothing is built yet. [`docs/PRD.md`](docs/PRD.md) says what is to be built and the rules it must hold, and [`SPEC.md`](SPEC.md) says what exists.

## How it's built

On Air is one Go app in two layers.

- **The core** is plain Go. It decides when to start and stop, keeps track of which pieces of a recording are whole, joins them and hands the file over. It builds and tests on any machine, and its rules are proved against small models.
- **The edge** touches macOS through cgo and small Objective-C files: the menu-bar dot, screen and sound capture. It does what the core tells it and reports what happened.

It is built with [Invariant](https://github.com/gitdek/invariant): each change starts as an issue, the rules a change must hold are ratified before it is built, and a gate checks every pull request.

## Working here

```bash
go vet ./...
go test ./...
tools/check-format
```

[`AGENTS.md`](AGENTS.md) has the rules for coding agents.
