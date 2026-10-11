# Security Policy

## Supported versions

Security fixes land on `main` and ship in the next release. There is no
long-term-support branch, so running an old build means running old fixes.

## Reporting a vulnerability

**Do not open a public issue.** Use GitHub's private reporting:

**Security → Report a vulnerability** on this repository
(<https://github.com/u007/ocode/security/advisories/new>).

Please include:

- affected version (`ocode --version`)
- your platform (macOS / Linux / Windows / WSL2) and Go version if you built from source
- what an attacker gains, and what they need in order to try it
- reproduction steps or a proof of concept
- whether any credential, transcript, or project file may already have been exposed

### What to expect

- Acknowledgement within a few days.
- An assessment once the report is reproduced, including whether it is accepted.
- A fix released, and the advisory published after a fix is available.

If a report needs a public fix before it can be discussed, say so in the
advisory draft and we will coordinate disclosure with you.

## Scope

In scope:

- permission bypass — a command or tool reaching an effect it should have asked about
- sandbox escape, in any of the four permission modes
- secret disclosure — an API key, token, or private key sent to a provider or written to a log
- transcript or session-file exposure to another local user or process
- remote-project (SSH/WSL) authentication or command-injection issues
- the web/desktop server, its auth middleware, and its terminal proxy

Out of scope:

- anything requiring you to already have unrestricted local shell access as the same user
- weaknesses in a third-party LLM provider, MCP server, or model
- the documented limits of the sandbox, which is write-integrity confinement only
  (reads and network stay open by design — see the security docs)
- social engineering of the agent into running something you would not have run yourself

## Hardening notes

If you accept pull requests from outside contributors, run untrusted work in a
container or VM. ocode's sandbox confines *writes* to the workspace; it is not a
confidentiality boundary.
