# Security Policy

## Reporting a Vulnerability

If you discover a security vulnerability in gozim, please report it responsibly by emailing the maintainers or opening a private security advisory on GitHub.

**Do not** open a public issue for security vulnerabilities.

## Supported Versions

Only the latest release on the `main` branch is supported with security updates.

## Scope

gozim reads ZIM archive files which may come from untrusted sources. The library includes:

- Binary parsing of ZIM headers and directory entries
- Zstandard decompression
- Optional fulltext search indexing via Bleve
- An optional HTTP server (`gozimhttpd`)

Relevant threats include malformed ZIM files causing panics, excessive memory allocation, or path traversal via entry paths. The HTTP server (`gozimhttpd`) is intended for local/trusted network use and does not implement authentication.

## Checksum Validation

`Archive.ValidateChecksum()` uses MD5 as specified by the ZIM format. This verifies file integrity (accidental corruption), not authenticity (tamper resistance). Do not rely on it for security-critical verification.
