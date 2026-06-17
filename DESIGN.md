# Granular — design

Granular grants **fine-grained, time-limited, human-approved** permissions to act
on third-party platforms (GitHub, and others later). Rather than handing an agent
a broad token, every permission is frozen by the platform gateway into a
grant request, approved by a human in a browser, and expires.

The defining constraint is a **separation of authority**: the component that runs
the consent screen and stores grants (the *authorization server*) is fully
domain-agnostic — it never holds a platform credential and understands no
permission vocabulary — while the component that holds the credential and knows
what a permission *means* (the *gateway*) never decides approval and never stores
grants.

## Components

- **`granular-client` (agent CLI)** — reads a gateway's permission schema, builds
  grant requests, submits them to the AS for approval, and runs operations once
  authorized. It holds no platform credential and no signing secret. Operations
  and proposals are attached to a **policy token** (a bearer credential).

- **`granular-github-gateway` (gateway / Resource Server)** — owns the GitHub
  credential and the permission vocabulary. It serves the permission **schema**,
  **signs** grant requests (see below), and **executes** operations — but only
  after the AS confirms the operation is authorized. The GitHub specifics live in
  `gateway-github`; the wire protocol and signing live in the generic `gateway`
  SDK so a second platform is a new schema + executors, not a new server.

- **`granular-auth-server` (authorization server, AS)** — the generic policy
  authority. It registers gateway HMAC credentials, accepts signed grant-request
  bundles (**proposals**) from clients, runs the human **consent screen**, stores
  **grants**, and answers **allow/deny** at operation time. It holds no platform
  credential, authors no policy, and renders the gateway's consent text verbatim.

- **`granular-policy` (admin CLI)** — mints, inspects, and destroys **policy
  tokens** against the AS admin credential. Policy lifecycle is deliberately an
  administrative concern, separate from the grant lifecycle the client drives.

## The signed-artifact model

The pivotal idea: **the gateway, not the AS, produces the consent content**, and
it freezes it at sign time. A signed grant request carries two index-aligned parts
(`internal/proposal`):

- a **Presentation** — the human-readable consent: a title, summary, and per-grant
  detail (friendly action labels, a typed resource label, and any conditions). It
  is what the approver reads.
- the **Policies** — the machine-enforced [Cedar](https://www.cedarpolicy.com/)
  rules that actually gate operations.

The gateway HMAC-signs both with the per-gateway secret it shares with the AS. The
client cannot forge or alter either; the AS verifies the signature, stores the
bytes, and **renders the Presentation verbatim** — it has no vocabulary with which
to interpret, expand, or add to it. The consent screen also exposes the raw Cedar
policies behind a disclosure, so a human can inspect exactly what is enforced.

A client builds a request two ways, both producing the same signed artifact:

- **Freeform** — raw actions over a scoped resource (`sign --actions … --resource
  … --match …`). Maximum flexibility.
- **Template** — a gateway-authored, parameterized permission shape (`sign
  --template … --bind …`), where parameters bind to scope or to a condition (or
  are fixed). Better readability on the consent screen.

## Request flow

```
client (agent)                gateway (GitHub)              AS + human (browser)
  | GET /api/schema --------------> |                                |
  |<------------ schema ----------- |                                |
  | POST /api/grant-requests/sign-> | freeze Presentation + Policies |
  |<-- signed (HMAC over both) ---- | sign with shared secret        |
  | POST /api/proposals ----------------------------------------->   | verify sig,
  |<----------- approval_url, expires_at ------------------------    | store pending
  |   (print URL, exit)                                              |
  |                                  GET  /proposal/{id} ----------> | login, render
  |                                  POST /proposal/{id} <---------- | pick TTL, approve
  |                                                                  | grant -> token
  | POST /api/operations ---------> | POST /api/verify ------------> | authorized?
  |                                  |<--------- allow/deny --------- |
  |<----------- result ------------ | execute with credential        |
```

Approval happens **out-of-band**: the client submits and exits; it does not poll.
Grants and proposals are persisted in a **bbolt** database, so decisions survive a
restart, and re-running an operation after approval simply succeeds.

## HTTP API

**Authorization server** (`auth_server/server`):

| Method | Path                     | Purpose                                                        |
|--------|--------------------------|----------------------------------------------------------------|
| PUT    | `/api/policy`            | Mint a policy token. **Admin-gated.**                          |
| GET    | `/api/policy/{token}`    | Inspect a token's grants. **Admin-gated.**                     |
| DELETE | `/api/policy/{token}`    | Destroy a token and its grants. **Admin-gated.**              |
| POST   | `/api/proposals`         | Submit a signed grant-request bundle; returns approval URL + expiry. |
| POST   | `/api/verify`            | Gateway asks whether a policy token authorizes an operation.   |
| GET    | `/proposal/{id}`         | Human consent page (renders the gateway Presentation verbatim). |
| POST   | `/proposal/{id}`         | Approve (with a grant TTL) or reject.                          |
| GET    | `/activity`              | Active grants + request history.                              |
| GET    | `/auth/github/{login,callback,logout}` | GitHub OAuth login for the consent pages.        |
| GET    | `/`, `/static/…`         | Landing page and embedded assets.                             |

**Gateway** (`gateway/server`):

| Method | Path                          | Purpose                                              |
|--------|-------------------------------|------------------------------------------------------|
| GET    | `/api/schema`                 | The permission schema (resources, actions, templates, operations). |
| POST   | `/api/grant-requests/sign`    | Freeze a grant request into a signed (Presentation, Policies). |
| POST   | `/api/operations`             | Run an operation: verify with the AS, then execute.  |

## Consent and authentication

The consent pages can be protected by a GitHub OAuth2 authorization-code login
(`auth_server/server/auth.go`), enabled when `auth.client_id` and
`auth.client_secret_file` are set (callback `<base_url>/auth/github/callback`).
There is **no global allowlist**: each proposal names an **approver email**, and
only the human whose verified GitHub email matches may decide it. The session is
an HMAC-signed, HttpOnly cookie.

The gateway↔AS channel is authenticated by a **per-gateway HMAC secret**: the
gateway sends its id in `X-Gateway-ID` and an HMAC over the body in
`X-Gateway-Signature`; the AS verifies against the secret registered for that id
and rejects an unknown or wrongly-signed gateway with `401`.

## Policy tokens and admin

A **policy token** is the bearer credential that grants attach to. Minting,
inspecting, and destroying tokens is gated by the AS **admin token**
(`admin_token_file`), which the `granular-policy` CLI presents. The gate is
**fail-closed**: when no admin token is configured, policy administration is
disabled (`503`); a wrong token is `401`. An administrator mints a token, hands it
to a client (via the client's `token_file`), and the client then submits proposals
and runs operations under it without any admin credential.

## Expiry

Two independent clocks, both enforced:

- **Grants** carry a TTL chosen by the approver; a background **janitor**
  (`cleanup_interval`) purges expired grants.
- **Proposals** (pending grant requests) carry a `request_ttl`: a request not
  acted on within that window is automatically revoked and the agent must request
  again. Expiry is enforced authoritatively at approve/reject time, reflected
  lazily in the consent and activity views, and persisted by the janitor for
  requests no one ever opened.

## Security properties

- **The AS is domain-agnostic.** It holds no permission vocabulary, authors no
  Cedar, and ships no catalog. It stores the gateway's signed (Presentation,
  Policies) opaquely and renders the Presentation verbatim — it cannot add to or
  reinterpret what the human approves.
- **The AS holds no platform credential.** Credentials live only on the gateway.
- **The client holds no signing secret.** Only the gateway and the AS share the
  per-gateway HMAC secret; the client cannot forge a grant request.
- **Secrets are never inline.** Every secret is referenced by a `*_file` path read
  at load time (gateway HMAC secret, GitHub PAT, AS admin token, OAuth secrets).
- **Approval is bound to identity.** A proposal is decided only by the human whose
  verified GitHub email matches its named approver.

## Layout

```
cmd/granular-client/          agent CLI entrypoint (main.go only)
cmd/granular-auth-server/     AS entrypoint
cmd/granular-github-gateway/  GitHub gateway entrypoint
cmd/granular-policy/          policy admin CLI entrypoint
clientcli/                    client command tree (catalog, template, op, sign, propose)
client/                       client SDK: proposals, operations, policy admin
gateway/                      generic gateway SDK: schema, sign, present, verify
gateway/asclient/             gateway's client for the AS verify call
gateway-github/               GitHub gateway: schema, templates, operation specs
gateway-github/internal/      GitHub-only, unimportable from outside the gateway:
  catalog/                      GitHub permission vocabulary (resources, actions)
  authz/                        Cedar GitHub entity world + capability→policy
  operations/                   operation framework + GitHub operation implementations
auth_server/config/           AS YAML configuration
auth_server/server/           AS HTTP handlers, consent UI, GitHub-OAuth login, eval
auth_server/server/web/       embedded consent/activity templates + stylesheet
auth_server/store/            grants + proposals store (bbolt)
internal/proposal/            the signed (Presentation, Policies) artifact
internal/verify/              generic, domain-agnostic gateway↔AS verify wire types
internal/api/                 shared wire types
```
