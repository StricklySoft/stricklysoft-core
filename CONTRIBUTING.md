# Contributing to StricklySoft Core SDK

Thank you for your interest in contributing to the StricklySoft Core SDK!

## Development Setup

1. Clone the repository:
   ```bash
   git clone https://github.com/StricklySoft/stricklysoft-core.git
   cd stricklysoft-core
   ```

2. Install dependencies:
   ```bash
   make deps
   ```

3. Run tests:
   ```bash
   make test
   ```

## Code Style

- Follow standard Go conventions
- Run `make lint` before submitting PRs
- Ensure all tests pass with `make test`

## Pull Request Process

1. Create a feature branch from `dev`
2. Make your changes
3. Add or update tests as needed
4. Update documentation if applicable
5. Submit a PR to the `dev` branch

## Commit Messages

Use conventional commits:
- `feat:` for new features
- `fix:` for bug fixes
- `docs:` for documentation changes
- `test:` for test changes
- `refactor:` for refactoring
- `chore:` for maintenance tasks

## Running Integration Tests

Integration tests require Docker:

```bash
make test-integration
```

## Questions?

Open a GitHub Discussion or reach out to the maintainers.
