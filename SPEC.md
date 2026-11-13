# On Air: what is built

This page describes On Air as it is. [`docs/PRD.md`](docs/PRD.md) describes what it is meant to become.

Nothing of the recorder is built yet. The repository holds:

- A Go module, `github.com/gitdek/onair`, with no code beyond its package comment.
- Its checks, in `.invariant/repository.json`: formatting, vet, build and tests, run in a pinned Go image.
- Its gate, `.github/workflows/gate.yml`, which runs those checks on every pull request and will verify each modeled project once there is one.
- The README's graphics, drawn by `docs/assets/generate.py` in a light and a dark theme.
