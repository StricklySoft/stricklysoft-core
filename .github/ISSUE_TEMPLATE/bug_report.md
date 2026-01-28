---
name: Bug Report
about: Report a bug in the StricklySoft Core SDK
title: "[BUG] "
labels: bug
assignees: ''
---

<!--
Before submitting a bug report:
1. Search existing issues to avoid duplicates
2. Ensure you're using a supported Go version (1.22+)
3. Verify the issue persists with the latest SDK version
4. Do NOT include sensitive information (API keys, passwords, internal URLs)
-->

## Describe the Bug

A clear and concise description of what the bug is.

## To Reproduce

Steps to reproduce the behavior:

1. Initialize/configure '...'
2. Call method '...' with parameters '...'
3. Observe error/unexpected behavior

## Expected Behavior

A clear and concise description of what you expected to happen.

## Actual Behavior

A clear and concise description of what actually happened.

## Environment

Please complete the following information:

- **Go version**: [e.g., 1.23.0] (run `go version`)
- **SDK version**: [e.g., v1.0.0] (check go.mod)
- **Operating System**: [e.g., Ubuntu 22.04, macOS 14.0, Windows 11]
- **Architecture**: [e.g., amd64, arm64]

## Minimal Reproducible Example

```go
package main

import (
    // Include necessary imports
)

func main() {
    // Minimal code that reproduces the issue
    // Remove any business logic not related to the bug
}
```

## Error Output / Stack Trace

```
Paste the complete error message and stack trace here.
Do NOT redact line numbers or file names.
DO redact any sensitive information (keys, passwords, internal URLs).
```

## Logs

<details>
<summary>Relevant log output (click to expand)</summary>

```
Paste relevant log output here.
Redact any sensitive information.
```

</details>

## Possible Cause

If you have investigated the issue, share your findings here.

## Possible Solution

If you have a suggested fix, describe it here.

## Workaround

If you found a workaround, describe it here to help others.

## Impact

How severely does this bug affect your use of the SDK?

- [ ] **Critical**: Blocks production use, no workaround available
- [ ] **High**: Significantly impacts functionality, workaround is difficult
- [ ] **Medium**: Impacts functionality, workaround is available
- [ ] **Low**: Minor inconvenience, easy workaround

## Additional Context

Add any other context about the problem here (screenshots, related issues, external references).

## Regression

- [ ] This worked in a previous version of the SDK
  - Last working version: [e.g., v0.9.0]
