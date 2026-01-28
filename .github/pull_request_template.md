<!--
Thank you for contributing to the StricklySoft Core SDK!

Please ensure your PR:
1. Follows the project's coding standards
2. Includes appropriate tests
3. Does not expose sensitive information
4. Has a clear, descriptive title
-->

## Description

<!-- Provide a clear and concise description of your changes -->

## Related Issue

<!-- Link to the issue this PR addresses. Use "Fixes #123" to auto-close the issue -->
Fixes #

## Type of Change

<!-- Check all that apply -->

- [ ] Bug fix (non-breaking change that fixes an issue)
- [ ] New feature (non-breaking change that adds functionality)
- [ ] Breaking change (fix or feature that would cause existing functionality to change)
- [ ] Performance improvement
- [ ] Code refactoring (no functional changes)
- [ ] Documentation update
- [ ] CI/CD changes
- [ ] Dependency update

## Breaking Changes

<!-- If this is a breaking change, describe the impact and migration path -->

- [ ] This PR introduces breaking changes

<details>
<summary>Breaking change details (if applicable)</summary>

### What breaks?

<!-- Describe what existing functionality will break -->

### Migration guide

<!-- Provide steps for users to migrate their code -->

```go
// Before (old API)

// After (new API)
```

</details>

## Testing

<!-- Check all testing steps completed -->

### Test Coverage

- [ ] Unit tests added for new functionality
- [ ] Unit tests updated for modified functionality
- [ ] All existing unit tests pass (`go test ./...`)
- [ ] Tests run with race detector (`go test -race ./...`)
- [ ] Test coverage maintained or improved

### Integration Testing

- [ ] Integration tests added/updated (if applicable)
- [ ] Manual testing completed
- [ ] Tested on Go 1.22
- [ ] Tested on Go 1.23

### Test Evidence

<!-- Provide output from test runs -->
<details>
<summary>Test output (click to expand)</summary>

```
# Paste test output here
go test -v -race ./...
```

</details>

## Code Quality Checklist

<!-- Ensure all items are completed before requesting review -->

### General

- [ ] Code follows project style guidelines
- [ ] Self-reviewed my own code
- [ ] No unnecessary comments or dead code
- [ ] No hardcoded values that should be configurable
- [ ] Error messages are clear and actionable

### Documentation

- [ ] Public functions/types have GoDoc comments
- [ ] Complex logic is documented with inline comments
- [ ] README updated (if applicable)
- [ ] CHANGELOG updated (if applicable)

### Security

- [ ] No sensitive data (API keys, passwords) in code
- [ ] Input validation added where necessary
- [ ] No new security vulnerabilities introduced
- [ ] Dependencies are from trusted sources

### Performance

- [ ] No unnecessary allocations or copies
- [ ] No blocking operations without timeouts
- [ ] Benchmarks added for performance-critical code (if applicable)

## Lint & Build

- [ ] `golangci-lint run` passes with no errors
- [ ] `go build ./...` succeeds
- [ ] `go mod tidy` has been run
- [ ] No new warnings generated

## Screenshots / Recordings

<!-- If applicable, add screenshots or recordings to demonstrate changes -->

## Additional Notes

<!-- Any additional information reviewers should know -->

## Reviewer Checklist

<!-- For reviewers - do not modify as PR author -->

- [ ] Code review completed
- [ ] Changes align with project architecture
- [ ] Test coverage is adequate
- [ ] Documentation is sufficient
- [ ] No security concerns identified
