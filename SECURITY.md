# Security policy

PiWorker runs commands, drives hardware, holds credentials, issues its own
certificate authority and lets AI agents call flows. Reports about any of that
are welcome.

## Reporting a vulnerability

**Do not open a public issue for a security problem.** Report it privately
through GitHub: on the repository's **Security** tab choose
[**Report a vulnerability**](https://github.com/Pegasus8/piworker/security/advisories/new).

Please include:

- the PiWorker version (`piworker -version`) and how it is installed
  (`piworkerctl`, Docker, a binary);
- what an attacker needs (network access, a session, an agent token and its
  scopes) and what they gain;
- steps or a flow definition that reproduce it. Remove real tokens, passwords
  and webhook URLs.

You will get an answer as soon as the maintainer can read it; PiWorker is a
volunteer project without a guaranteed response time. Please allow time for a
fix before publishing details.

## Supported versions

Fixes go into the latest release of the current implementation. The
`v0.1.0-alpha.1` release from 2020 is a different program and is not
maintained.

## What counts

In scope, for example: bypassing sign-in, the request guard or the Host
allowlist; reading secrets, or agent output revealing hidden values beyond the
limits already documented; an agent acting outside its token's scopes, grants
or the agent deploy level; escaping the expression sandbox or the dry-run
sandbox; reaching private addresses through a node with **Allow Private
Addresses** off; leaking the CA key.

Documented limits are not vulnerabilities, although suggestions are welcome:
secrets are stored without encryption at rest; someone who can create command
flows controls the PiWorker user and can read the CA key; an agent whose text
comes back to it can confirm guesses of short secrets; webhooks served over
plain HTTP carry their token unencrypted. See the README's
[security notes](README.md#security-notes) and [HTTPS](README.md#https), and
the decisions in [docs/adr](docs/adr/README.md).
