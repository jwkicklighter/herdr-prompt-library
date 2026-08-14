# Contributing

Contributions are welcome. Please open an issue before starting a large change so the approach can be discussed early.

## Development

Herdr Prompt Library requires Go 1.25 or later. Before submitting a change, run:

```sh
gofmt -w path/to/changed/files.go
go vet ./...
go test ./...
go build ./...
```

Update `README.md` when a change affects installation, configuration, controls, or other user-visible behavior.

## Commit messages

Use [Conventional Commits](https://www.conventionalcommits.org/) for commit messages and pull request titles. Pull-request commits are checked in CI.

```text
feat: add prompt import support
fix(search): preserve the selected result
docs: clarify local prompt setup
```

Allowed types are `build`, `chore`, `ci`, `docs`, `feat`, `fix`, `perf`, `refactor`, `revert`, `style`, and `test`. Use an optional scope when it adds useful context. Mark breaking changes with `!` before the colon or a `BREAKING CHANGE:` footer.

## AI-assisted contributions

The use of AI tools is completely welcome. Contributors remain responsible for understanding, reviewing, testing, and licensing everything they submit.

Disclose AI assistance in the pull request. Name the tool or tools and briefly describe how they were used. If no AI tools were used, state that explicitly in the provided PR template section.

## Pull requests

Keep pull requests focused and explain the user-visible effect of the change. Complete the pull request template, including its validation checklist and AI assistance disclosure, and ensure all required CI checks pass.
