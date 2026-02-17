# Security Policy

## Supported Versions

| Version | Supported          |
|---------|--------------------|
| 1.0.x   | Yes                |
| < 1.0   | No                 |

## Reporting a Vulnerability

If you discover a security vulnerability in logtap, please report it responsibly.

**Do not open a public GitHub issue for security vulnerabilities.**

Instead, email **security@ppiankov.dev** with:

- Description of the vulnerability
- Steps to reproduce
- Affected version(s)
- Impact assessment (what an attacker could achieve)

You should receive an acknowledgement within 48 hours. We aim to release a fix within 7 days for critical issues.

## Scope

logtap handles log data that may contain sensitive information. The following areas are in scope:

- PII redaction bypass (data that should be redacted reaching disk)
- Unauthorized access to capture data via the HTTP receiver
- Container escape or privilege escalation via sidecar injection
- Credential leakage in CLI output, logs, or error messages
- Path traversal in capture directory operations

## Out of Scope

- Denial of service against the receiver (logtap is designed for ephemeral load testing, not production traffic)
- Vulnerabilities in third-party dependencies (report those upstream, but let us know so we can update)
- Issues requiring physical access to the machine running logtap

## Disclosure

We follow coordinated disclosure. After a fix is released, we will:

1. Credit the reporter (unless anonymity is requested)
2. Publish a security advisory on GitHub
3. Release a patched version
