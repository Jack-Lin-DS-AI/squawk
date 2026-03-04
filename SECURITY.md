# Security Policy

## Supported Versions

| Version | Supported |
|---------|-----------|
| 0.1.x   | Yes       |

## Reporting a Vulnerability

If you discover a security vulnerability, please report it responsibly:

1. **Do NOT** open a public GitHub issue
2. Email: [open an issue with the "security" label on GitHub]
3. Include:
   - Description of the vulnerability
   - Steps to reproduce
   - Potential impact
   - Suggested fix (if any)

## Security Model

Squawk runs as a localhost-only HTTP server. Key security properties:

- **Localhost only**: The server binds to `localhost` by default
- **Fail-open**: If squawk is unreachable, Claude Code continues normally
- **No secrets stored**: Configuration contains no credentials
- **Minimal dependencies**: Only `cobra` and `yaml.v3`

## Scope

The following are in scope for security reports:

- Command injection via rule templates or notifications
- Unauthorized access to admin endpoints
- Path traversal in rule file handling
- Denial of service via resource exhaustion
- Information disclosure through log files
