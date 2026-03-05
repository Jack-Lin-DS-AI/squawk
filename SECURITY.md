# Security Policy

## Supported Versions

| Version | Supported |
|---------|-----------|
| 0.1.x   | Yes       |

## Reporting a Vulnerability

If you discover a security vulnerability, please report it responsibly:

1. **Do NOT** open a public GitHub issue
2. Report via [GitHub Security Advisory](https://github.com/Jack-Lin-DS-AI/squawk/security/advisories/new)
3. Include:
   - Description of the vulnerability
   - Steps to reproduce
   - Potential impact
   - Suggested fix (if any)

## Security Model

Squawk runs as a localhost-only HTTP server. Key security properties:

- **Localhost only**: The server binds to `localhost` by default
- **Fail-open**: If squawk is unreachable, Claude Code continues normally
- **Admin token auth**: Admin endpoints require a Bearer token stored in `.squawk/admin.token`
- **No secrets stored**: Configuration contains no credentials
- **Minimal dependencies**: Only `cobra` and `yaml.v3`

## Scope

The following are in scope for security reports:

- Command injection via rule templates or notifications
- Unauthorized access to admin endpoints
- Path traversal in rule file handling
- Denial of service via resource exhaustion
- Information disclosure through log files
