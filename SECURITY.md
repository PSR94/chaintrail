# Security policy

Chaintrail is integrity-sensitive software. Please avoid filing public reports that include private signing keys, credentials, production journal contents, or exploit details that would expose a real deployment.

## Reporting a vulnerability

Use GitHub private vulnerability reporting / a private security advisory for this repository when available. Include the smallest safe reproduction that demonstrates the issue and identify whether it affects:

- journal integrity validation;
- canonical hashing;
- repair behavior;
- checkpoint/signature verification;
- key handling;
- filesystem locking/durability assumptions;
- CLI error classification.

If private reporting is unavailable, open a minimal issue requesting a private contact path without posting sensitive reproduction data.

## Supported versions

Before the first stable release, security fixes are applied to the current `main` branch and documented in `CHANGELOG.md`. Tagged release support policy will be defined when stable release lines exist.

## Threat model

The complete trust model, attacker capabilities, key-custody guidance, and deployment checklist are documented in [`docs/SECURITY.md`](docs/SECURITY.md).

Chaintrail provides tamper evidence, not confidentiality, malware resistance, kernel trust, remote availability, or per-event actor authentication.
