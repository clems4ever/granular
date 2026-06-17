# Granular — design

Granular grants **fine-grained, time-limited, human-approved** permissions to act
on third-party platforms (GitHub, and others later). Rather than handing an agent
a broad token, every permission is frozen by the platform resource server into a
grant request, approved by a human in a browser, and expires.

The defining constraint is a **separation of authority**: the component that runs
the consent screen and stores grants (the *authorization server*) is fully
domain-agnostic — it never holds a platform credential and understands no
permission vocabulary — while the component that holds the credential and knows
what a permission *means* (the *resource server*) never decides approval and never stores
grants.

## Components

- **`granular-client` (agent CLI)** — reads a resource server's permission schema, builds
  grant requests, submits them to the AS for approval, and runs operations once
  authorized. It holds no platform credential and no signing secret. Operations
  and proposals are attached to a **subject token** (a bearer credential).

- **`granular-github-resource-server` (resource server / Resource Server)** — owns the GitHub
  credential and the permission vocabulary. It serves the permission **schema**,
  **signs** grant requests (see below), and **executes** operations — but only
  after the AS confirms the operation is authorized. The GitHub specifics live in
  `resourceserver-github`; the wire protocol and signing live in the generic `resourceserver`
  SDK so a second platform is a new schema + executors, not a new server.

- **`granular-auth-server` (authorization server, AS)** — the generic policy
  authority. It registers resource server HMAC credentials, accepts signed grant-request
  bundles (**proposals**) from clients, runs the human **consent screen**, stores
  **grants**, and answers **allow/deny** at operation time. It holds no platform
  credential, authors no policy, and renders the resource server's consent text verbatim.

- **`granular-subject` (admin CLI)** — mints, inspects, and destroys **subject
  tokens** against the AS admin credential. Subject lifecycle is deliberately an
  administrative concern, separate from the grant lifecycle the client drives.

## The signed-artifact model

The pivotal idea: **the resource server, not the AS, produces the consent content**, and
it freezes it at sign time. A signed grant request carries two index-aligned parts
(`internal/proposal`):

- a **Presentation** — the human-readable consent: a title, summary, and per-grant
  detail (friendly action labels, a typed resource label, and any conditions). It
  is what the approver reads.
- the **Policies** — the machine-enforced [Cedar](https://www.cedarpolicy.com/)
  rules that actually gate operations.

The resource server HMAC-signs both with the per-resource-server secret it shares with the AS. The
client cannot forge or alter either; the AS verifies the signature, stores the
bytes, and **renders the Presentation verbatim** — it has no vocabulary with which
to interpret, expand, or add to it. The consent screen also exposes the raw Cedar
policies behind a disclosure, so a human can inspect exactly what is enforced.

A client builds a request two ways, both producing the same signed artifact:

- **Freeform** — raw actions over a scoped resource (`sign --actions … --resource
  … --match …`). Maximum flexibility.
- **Template** — a resource server-authored, parameterized permission shape (`sign
  --template … --bind …`), where parameters bind to scope or to a condition (or
  are fixed). Better readability on the consent screen.

## Request flow

```
client (agent)                resource server (GitHub)              AS + human (browser)
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
| PUT    | `/api/subject`            | Mint a subject token. **Admin-gated.**                          |
| GET    | `/api/subject/{token}`    | Inspect a token's grants. **Admin-gated.**                     |
| DELETE | `/api/subject/{token}`    | Destroy a token and its grants. **Admin-gated.**              |
| GET    | `/api/subject/me`         | A subject reads its OWN grants, authenticated by its subject token. |
| POST   | `/api/proposals`         | Submit a signed grant-request bundle; returns approval URL + expiry. |
| POST   | `/api/verify`            | resource server asks whether a subject token authorizes an operation.   |
| GET    | `/api/activity`          | Full cross-subject grant inventory + history. **Admin-gated.**  |
| GET    | `/proposal/{id}`         | Human consent page (renders the resource server Presentation verbatim). |
| POST   | `/proposal/{id}`         | Approve (with a grant TTL) or reject.                          |
| GET    | `/auth/github/{login,callback,logout}` | GitHub OAuth login for the consent pages.        |
| GET    | `/`                      | The single main page: a signed-in approver's own request/decision history, else a landing. |
| GET    | `/openapi.yaml`, `/docs` | The OpenAPI spec and its Redoc-rendered API reference. |
| GET    | `/static/…`              | Embedded assets.                                              |

**resource server** (`resourceserver/server`):

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

The resource server↔AS channel is authenticated by a **per-resource-server HMAC secret**: the
resource server sends its id in `X-Resource-Server-ID` and an HMAC over the body in
`X-Resource-Server-Signature`; the AS verifies against the secret registered for that id
and rejects an unknown or wrongly-signed resource server with `401`.

## Subject tokens and admin

A **subject token** is the bearer credential that grants attach to. Minting,
inspecting, and destroying tokens is gated by the AS **admin token**
(`admin_token_file`), which the `granular-subject` CLI presents. The gate is
**fail-closed**: when no admin token is configured, subject administration is
disabled (`503`); a wrong token is `401`. An administrator mints a token, hands it
to a client (via the client's `token_file`), and the client then submits proposals
and runs operations under it without any admin credential.

### Who sees what

Visibility is scoped to three distinct audiences — no single view exposes everything
to everyone:

- **A subject** reads only its OWN grants, at `GET /api/subject/me`, authenticated by
  its subject token (the bearer it already holds — no admin credential). A sandboxed
  agent can introspect what it currently holds and nothing else.
- **An approver** (human, GitHub login) sees only the requests that name them: their
  own pending requests and decision history at `/activity`. They never see other
  approvers' history or the global grant inventory. When consent authentication is
  disabled there is no approver identity, so the page is unavailable (`404`).
- **An operator** (admin token) gets the full cross-subject grant inventory and
  history at `GET /api/activity` (the `granular-subject activity` command). This is the
  only view that spans subjects, and it is admin-gated like the rest of `/api/subject`.

## Expiry

Two independent clocks, both enforced:

- **Grants** carry a TTL chosen by the approver; a background **janitor**
  (`cleanup_interval`) purges expired grants.
- **Proposals** (pending grant requests) carry a `grant_request_ttl`: a request not
  acted on within that window is automatically revoked and the agent must request
  again. Expiry is enforced authoritatively at approve/reject time, reflected
  lazily in the consent and activity views, and persisted by the janitor for
  requests no one ever opened.

## Security properties

- **The AS is domain-agnostic.** It holds no permission vocabulary, authors no
  Cedar, and ships no catalog. It stores the resource server's signed (Presentation,
  Policies) opaquely and renders the Presentation verbatim — it cannot add to or
  reinterpret what the human approves.
- **The AS holds no platform credential.** Credentials live only on the resource server.
- **The client holds no signing secret.** Only the resource server and the AS share the
  per-resource-server HMAC secret; the client cannot forge a grant request.
- **Secrets are never inline.** Every secret is referenced by a `*_file` path read
  at load time (resource server HMAC secret, GitHub PAT, AS admin token, OAuth secrets).
- **Approval is bound to identity.** A proposal is decided only by the human whose
  verified GitHub email matches its named approver.

## Layout

```
cmd/granular-client/          agent CLI entrypoint (main.go only)
cmd/granular-auth-server/     AS entrypoint
cmd/granular-github-resource-server/  GitHub resource server entrypoint
cmd/granular-subject/          subject admin CLI entrypoint
clientcli/                    client command tree (catalog, template, op, sign, propose)
client/                       client SDK: proposals, operations, subject admin
resourceserver/                      generic resource server SDK: schema, sign, present, verify
resourceserver/asclient/             resource server's client for the AS verify call
resourceserver-github/               GitHub resource server: schema, templates, operation specs
resourceserver-github/internal/      GitHub-only, unimportable from outside the resource server:
  catalog/                      GitHub permission vocabulary (resources, actions)
  authz/                        GitHub requirement + resource-reference primitives
  operations/                   operation framework + GitHub operation implementations
auth_server/config/           AS YAML configuration
auth_server/server/           AS HTTP handlers, consent UI, GitHub-OAuth login, eval
auth_server/server/web/       embedded consent/activity templates + stylesheet
auth_server/store/            grants + proposals store (bbolt)
internal/proposal/            the signed (Presentation, Policies) artifact
internal/verify/              generic, domain-agnostic resource server↔AS verify wire types
internal/api/                 shared wire types
```
