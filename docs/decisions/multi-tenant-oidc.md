# Multi-tenant OIDC providers are not supported

**Status:** decided, implemented
**Date:** 2026-08-07
**Applies to:** `internal/oidc`, `main.go` OIDC startup wiring

## Decision

Mikroview refuses to enable SSO when `oidc.issuerUrl` points at a
multi-tenant provider — Google, Apple, Microsoft's shared
`/common`, `/organizations` and `/consumers` endpoints, and any other
issuer whose user population is the general public. The refusal is
unconditional and cannot be overridden by configuration.

Only self-hosted issuers are supported: Authentik, Keycloak, Zitadel, or
an Entra **single-tenant** issuer URL (`.../<tenant-guid>/v2.0`), which
is correctly *not* treated as multi-tenant because it genuinely does
scope logins to one organisation.

## Why

Mikroview's OIDC support rests on one property: **the issuer URL is
itself the access control.** Every ID token is verified against the
configured issuer's own signing keys and against the client ID, so
pointing `issuerUrl` at a directory you run means only accounts in that
directory can sign in.

That property is simply false for a public provider. Every Google
account on earth produces a valid token against `accounts.google.com`.
The issuer stops narrowing anything, and since the first account to
register becomes an admin, an unrestricted deployment hands admin to
whoever reaches the login page first.

A safe configuration for a public provider does exist — pin a claim that
identifies the organisation (see the revival section below) — and it was
built, tested, and shipped. It was then removed on purpose.

The reasoning is about who mikroview is for. This is a tool for home and
small self-hosters. Supporting public IdPs means the safety of every such
deployment depends on an operator understanding the difference between
"authenticated by Google" and "authenticated as someone I trust", and
then getting an extra claim restriction exactly right. Get it wrong and
the failure is silent, total, and indistinguishable from working
correctly until someone else logs in. A narrower promise that cannot be
misconfigured is worth more than a broader one that can.

The cost is real and accepted: a small business on Google Workspace or
Microsoft 365 cannot use SSO with mikroview. They can still use local
accounts, or run any self-hosted IdP in front of their existing
directory.

## What was kept

Most of the work. `oidc.Policy` — `allowedGroups`, `groupsClaim`,
`allowedEmails`, `allowedEmailDomains`, `requiredClaims` — remains fully
implemented, documented, and tested, because it is independently useful
for scoping access *within* a self-hosted directory. An Authentik that
also serves other household members or other applications vouches for
accounts that have no business reading firewall logs; `allowedGroups` is
how you narrow that.

`IsMultiTenantIssuer` was also kept, and is now the whole gate rather
than half a condition.

All the fail-closed properties that make `Policy` trustworthy are
unchanged and still tested: a missing or unreadable claim refuses rather
than passes, email domains match whole labels rather than string
suffixes, `email_verified` is required, the check runs before account
provisioning, and it re-runs on every login.

## Reversing this, if the trade is ever revisited

The removal was one clause. `internal/oidc/policy.go` gained
`AllowIssuer`, which `main.go` consults during OIDC startup.

To go back to "public providers allowed, but only when a restriction is
configured", change `AllowIssuer` to take the policy into account:

```go
// AllowIssuer reports whether SSO may be enabled against issuer.
// A multi-tenant provider is permitted only when policy narrows access
// beyond "any account at that issuer".
func AllowIssuer(issuer string, policy Policy) error {
	if IsMultiTenantIssuer(issuer) && !policy.Restricted() {
		return fmt.Errorf("%w: %s", ErrMultiTenantIssuer, issuer)
	}
	return nil
}
```

and update its one call site in `main.go`'s OIDC `switch` to
`oidc.AllowIssuer(cfg.OIDC.IssuerURL, oidcPolicy) != nil`, adjusting the
error message to say SSO is unavailable *until* a restriction is
configured rather than unsupported outright.

Nothing else needs to change — `Policy`, `Restricted()`,
`IsMultiTenantIssuer`, the config fields, and every test still exist.

The working configuration for each public provider, which was verified
against the real claim shapes before removal:

```yaml
# Google Workspace -- only accounts at example.com, not personal Gmail.
# Personal accounts carry no `hd` claim at all, so they are refused by
# the fail-closed missing-claim rule rather than by a value comparison.
oidc:
  issuerUrl: "https://accounts.google.com"
  clientId: "..."
  clientSecret: "..."
  publicBaseUrl: "https://mikroview.example.com"
  requiredClaims:
    hd: ["example.com"]

# Microsoft Entra -- only your tenant.
# Prefer the single-tenant issuer URL, which needs none of this and is
# already supported. requiredClaims.tid is only needed if you use the
# shared /common endpoint.
oidc:
  issuerUrl: "https://login.microsoftonline.com/common/v2.0"
  clientId: "..."
  clientSecret: "..."
  publicBaseUrl: "https://mikroview.example.com"
  requiredClaims:
    tid: ["00000000-0000-0000-0000-000000000000"]
```

Full history, including the review discussion that led here, is in
PR #149 and issue #145.
