# Contributing to Squawk

Thank you for your interest in contributing to Squawk!

## Development Setup

1. **Prerequisites**: Go 1.24+ installed
2. **Clone**: `git clone https://github.com/Jack-Lin-DS-AI/squawk.git`
3. **Build**: `make build`
4. **Test**: `make test`

## Making Changes

1. Fork the repository
2. Create a feature branch: `git checkout -b feat/my-feature`
3. Make your changes
4. Run tests: `make test`
5. Run linter: `make lint`
6. Commit with conventional commits: `feat: add new rule type`
7. Open a pull request

## Commit Messages

We use [conventional commits](https://www.conventionalcommits.org/):

- `feat:` new feature
- `fix:` bug fix
- `refactor:` code change that neither fixes a bug nor adds a feature
- `test:` adding or updating tests
- `docs:` documentation only
- `chore:` maintenance tasks

## Code Style

- Run `gofmt` and `goimports` before committing
- Follow standard Go conventions
- Accept interfaces, return structs
- Wrap errors with context: `fmt.Errorf("failed to X: %w", err)`

## Writing Rules

Custom rules are YAML files in the `rules/` directory. See the [Rules Catalog](docs/RULES_CATALOG.md) for examples and field reference.

## Testing

- Write table-driven tests
- Run with race detector: `go test -race ./...`
- Aim for 80%+ coverage on new code

## Reporting Issues

- Use GitHub Issues for bug reports and feature requests
- Include steps to reproduce for bugs
- Include squawk version (`squawk --version`)
