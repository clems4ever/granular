# Granular — design

Granular grants **fine-grained, time-limited, human-approved** permissions to
perform individual operations on third-party platforms (GitHub, Google
Calendar, Google Drive, …). Rather than handing an automated agent a broad token,
each concrete operation must be approved by a human, and the approval expires.

## Components

- **`granular` (CLI client)** — exposes one sub-command per granular operation.
  It does **not** hold any platform credentials. It asks the server to perform an
  operation; if the operation is not yet approved it shows the user an approval
  URL and waits.

- **`granular-server` (HTTP server)** — holds the platform credentials (for now a
  GitHub PAT supplied through the `GRANULAR_GITHUB_TOKEN` environment variable).
  It decides whether an operation is allowed, mints **delegation requests**,
  serves a small approval web page, records **grants**, and executes the
  operation when a live grant exists.

The server is **stateless with respect to the client**: it never makes the client
wait. Delegation requests and grants are persisted to a **bbolt** database on disk,
so a decision is made entirely **out-of-band** (in the browser) and survives a
server restart. The CLI does **not** poll — it submits, and either gets the result
(if already granted) or an approval URL to open, after which the user simply
re-runs the command.

## Authorization (Cedar)

Authorization is decided by the [Cedar](https://www.cedarpolicy.com/) policy engine
(`internal/authz`), not by exact-string key matching:

- Each **operation** declares its **requirements** — `(action, resource, context)`
  checks that must all pass (e.g. `issue.view` on `GitHub::Issue::"owner/name#7"`;
  a `--comments` view adds a second `comment.read` requirement). Resources, actions
  (and their roll-up groups like `read`/`issues.read`) come from the single
  `internal/catalog` manifest.
- A **grant** is one or more **Cedar policies** stored with an expiry
  (`internal/grants`, bbolt). `POST /api/operations` loads the active (non-expired)
  policies and asks the engine `AllowsAll(requirements)`.
- If allowed → execute. If not → mint **minimal permits** (one per requirement,
  scoped to the exact resource + content hash) and create a delegation request;
  the human approves and the policies are stored.
- **Pre-approval** (`POST /api/permissions`): a custom `PermissionsRequest` is
  translated to broader Cedar policies (e.g. `permit(action in [issues.read],
  resource in GitHub::Repo::"owner/name")`). One approval then covers every concrete
  operation the policy allows — `list` *and* `view*, across the repo — while writes
  outside the grant still require their own approval.

Writes stay content-scoped under Cedar via a `context` condition
(`when { context.body_hash == "…" }`) on the minimal permit, so re-running the same
write reuses the grant but a different payload needs fresh approval.

## Core concepts

- **Operation** — a concrete, parameterised action, e.g. `github.clone` with
  params `{repo}`. Each operation knows how to (a) declare its **requirements**
  (Cedar `action`/`resource`/`context` checks) and (b) execute itself server-side.

- **Requirement** — one authorization check, e.g. `issue.view` on
  `GitHub::Issue::"owner/name#7"`. See [Authorization (Cedar)](#authorization-cedar).

- **Delegation request** — created when an operation (or `PermissionsRequest`) is
  not authorized by the active policies. It captures the proposed Cedar policies, a
  generated id, and a `pending` status. The id is embedded in the approval URL
  `…/approve/{id}`.

- **Grant** — one or more Cedar policies stored on approval, each with an expiry.
  An operation is allowed iff the active (non-expired) policies authorize all of
  its requirements.

## Request flow

```
CLI                         server                         human (browser)
 |   POST /api/operations      |                                |
 |---------------------------->|  Cedar: AllowsAll(reqs)?       |
 |                             |  - no -> persist request, 202  |
 |<----------------------------|  {status:pending, approval_url}|
 |  print approval_url, EXIT   |                                |
 |                             |          GET /approve/{id} --> | shows operation
 |                             |          POST /approve/{id} <--| picks expiry, approves
 |                             |  persist grant (bbolt)         |
 |   POST /api/operations      |   (user re-runs the command)   |
 |---------------------------->|  live grant -> 200 {clone_url} |
 |<----------------------------|  brokered git-proxy URL        |
 |   git clone <clone_url>     |                                |
 |---------------------------->|  /git/... : re-check grant,    |
 |   (clone runs LOCALLY,      |  inject PAT, proxy to github   |
 |    files land on the CLI)   |======> github.com              |
 |<============================|  streamed pack data            |
```

The approval happens out-of-band; the CLI never blocks waiting for it. The clone
itself runs **on the client** — the server is only a credential-injecting git
proxy, so the working tree lands wherever the user pointed the CLI, and the PAT
never leaves the server. Because the grant is persisted, **re-running the CLI
command** after approval performs the clone.

## HTTP API

| Method | Path                  | Purpose                                                       |
|--------|-----------------------|---------------------------------------------------------------|
| POST   | `/api/operations`     | Attempt an operation. `200` with result, or `202` pending.    |
| POST   | `/api/permissions`    | Submit a custom scoped capability bundle; `202` pending.      |
| GET    | `/api/requests/{id}`  | Inspect a delegation request's status.                        |
| GET    | `/approve/{id}`       | Human-facing approval page (HTML form).                       |
| POST   | `/approve/{id}`       | Submit approval (decision + expiry) or rejection.             |
| any    | `/git/{owner}/{repo}.git/...` | Authenticating git proxy. Re-checks the repo's grant, injects the PAT, forwards read (upload-pack) traffic to github.com. Refuses receive-pack (push). |
| GET    | `/catalog`            | HTML capability catalog: resource hierarchy, verb lattice, CLI operations. |
| GET    | `/api/catalog`        | JSON capability manifest (for the agent).                   |

`POST /api/operations` request body:

```json
{ "type": "github.clone", "params": { "repo": "github.com/owner/name" } }
```

Pending response (`202`):

```json
{ "status": "pending", "request_id": "…", "approval_url": "http://host/approve/…" }
```

Authorized response (`200`) — the result carries the brokered proxy URL the client
clones from, not a server-side result:

```json
{ "status": "completed", "result": { "clone_url": "http://host/git/owner/name.git", "repo": "owner/name" } }
```

## First operation: `github.clone`

`granular github clone <repo> <dest> [--ref <ref>]` clones a GitHub repository
**on the client**, with the server acting as a credential-injecting git proxy:

1. The CLI calls `POST /api/operations`. With a live grant the server returns a
   `clone_url` pointing at its own `/git/owner/name.git` proxy endpoint.
2. The CLI runs a local `git clone <clone_url> <dest>` (plus `--branch <ref>` when
   `--ref` is given).
3. git's smart-HTTP requests hit the server proxy, which re-checks the grant for
   the repository, sets `Authorization` from the server-held PAT, and forwards to
   `https://github.com`. Pack data streams back to the client's git process.

The PAT never leaves the server and is never placed on a command line. The clone
is repo-scoped (see below). Push (`git-receive-pack`) is refused by the proxy.

## Second operation: `github.issue.list`

`granular github issue list <repo> [--state open|closed|all] [--limit N]` lists a
repository's issues. Unlike clone, this is **server-executed**: once a grant
exists, the server calls the GitHub REST API (`GET /repos/{repo}/issues`) with the
PAT and returns GitHub's response **verbatim** (every attribute, every item — note
GitHub's issues endpoint also includes pull requests) under `result.issues`; the
CLI text view shows a one-line summary, and `--json` emits the raw array. Nothing
is proxied because the result is structured data, not a stream the client owns.

The grant is scoped to the repository **and the requested state**
(`github.issue.list:owner/name?state=open`), so approving "list open issues" does
not authorise listing closed ones — a concrete example of the granular model.

## Third operation: `github.issue.view`

`granular github issue view <repo> <number> [--comments]` shows a single issue's
details (`gh issue view`). Also server-executed: on a live grant the server calls
`GET /repos/{repo}/issues/{number}` with the PAT and returns GitHub's issue object
**verbatim**. The CLI text view renders a few fields; `--json` emits the full raw
object. The grant is scoped to the **specific issue**
(`github.issue.view:owner/name#7`), so approving one issue does not authorise
viewing another.

`--comments` makes the server additionally call
`GET /repos/{repo}/issues/{number}/comments` and fold the raw comments array into
the result under the synthetic `comments_list` key. It is a **separate
requirement** (`comment.read`), authorized independently from `issue.view`, so
reading an issue's discussion is approved separately from its metadata while the CLI
surface still matches `gh issue view --comments`.

## Write operations: `github.issue.comment` and `github.issue.create`

`granular github issue comment <repo> <number> --body …` posts a comment
(`gh issue comment`); `granular github issue create <repo> --title … [--body …]
[--label …] [--assignee …]` opens an issue (`gh issue create`). Both are
server-executed `POST`s and return GitHub's created object verbatim.

Because they **mutate**, two things differ from the read operations:

- **Content-scoped grants.** The requirement carries a `context` hash of the
  submitted content (`body_hash`, `content_hash`, `change_hash`), and the minimal
  permit conditions on it (`when { context.body_hash == "…" }`). So the approver
  authorises *exactly* what gets written; changing the text requires a fresh
  approval. The approval page shows the full body/title and the Cedar policies.
- **Write scope required.** The server PAT (`GRANULAR_GITHUB_TOKEN`) needs write
  access to the repository, unlike the read-only list/view which work on public
  repos even unauthenticated.

The body can come from `--body` or `--body-file` (`-` reads stdin), mirroring `gh`.

## Edit and status operations: `issue edit` / `close` / `reopen`

Mirroring `gh issue edit` / `close` / `reopen`, but as **three separate operation
types** so the grants are independent — a grant to change status cannot edit the
issue's content, and vice-versa:

- **`github.issue.edit`** — `PATCH` of fields (`--title`, `--body`,
  `--add-label`/`--remove-label`, `--add-assignee`/`--remove-assignee`). Label and
  assignee changes are add/remove sets, so `Execute` first `GET`s the issue to merge
  against its current values. Content-scoped grant (hash of the requested changes).
- **`github.issue.close`** — `PATCH {state: closed}` with an optional
  `--reason` (`completed` / `not planned` → `state_reason`). Grant scoped to the
  issue (and reason).
- **`github.issue.reopen`** — `PATCH {state: open}`. Grant scoped to the issue.

Status changes (`close`/`reopen`) deliberately do **not** accept a `--comment` or
touch any field — posting a comment is its own `github.issue.comment` grant. This
keeps "change status" a strictly separate, minimal permission from "edit content".

### Raw pass-through

Both issue operations decode GitHub's response into generic JSON (`[]any` /
`map[string]any`) and return it unchanged, rather than projecting a curated subset.
So `--json` matches what the GitHub API returns. The text renderers read GitHub's
native field names (`user.login`, `html_url`, `labels[].name`). Trade-off: a
delegated caller with `--json` sees every attribute GitHub returns — broaden the
operation if you need to *narrow* what a grant exposes.

## Two execution models

The `Operation.Execute` contract is the same, but operations fall into two shapes:

- **Server-executed** (e.g. `github.issue.list`): `Execute` does the work using the
  PAT and returns the result, which the CLI renders.
- **Client-fulfilled / brokered** (e.g. `github.clone`): `Execute` does no real
  work; it returns a brokered URL (the git proxy) and the CLI performs the action
  locally through it. Used when the client must own the output (a working tree) or
  the protocol is a stream.

## Decisions taken for this first iteration (and why)

- **bbolt on-disk store** for approved Cedar policies and delegation requests (two
  buckets: `requests`, `policies`). This keeps the server stateless toward the
  client and lets approvals happen out-of-band and survive restarts. Path via
  `GRANULAR_DB` (default `<workspace>/granular.db`).
- **Cedar for authorization** (`internal/authz`), with `internal/catalog` as the
  single source for resources, actions and the verb lattice — shared by the engine,
  the `/catalog` page, and the `/api/catalog` manifest. Principal identity is a
  fixed `GitHub::Agent::"agent"` for now (per-agent identity is future work).
- **Server is a git proxy, not a git client.** The clone runs on the CLI (shelling
  out to the user's `git`) so the working tree lands on the client; the server only
  brokers credentials. This keeps the token server-side and avoids shipping repo
  bytes back through the API.
- **PAT via env var** (`GRANULAR_GITHUB_TOKEN`) — "on the server for now", per the
  brief. Per-user / OAuth credential brokering is future work.
- **CLI does not poll.** It prints the approval URL and exits; the user approves
  out-of-band and re-runs the command. This keeps the server stateless and avoids
  long-lived client connections.
- **Clone grants are repo-scoped** (`github.clone:owner/name`). A `git clone`
  negotiates all refs in one exchange, so per-ref enforcement at the proxy is not
  meaningful; `--ref` only controls the client-side checkout. The destination path
  is purely client-side and never reaches the server.

## Layout

```
cmd/granular/          thin CLI entrypoint (main.go only)
cmd/granular-server/   server entrypoint (registers operations)
internal/cli/          CLI command tree (one file per command) + request.go (request/catalog)
internal/api/          wire types shared by client & server (incl. PermissionsRequest)
internal/catalog/      single-source capability manifest (resources, actions, lattice)
internal/authz/        Cedar engine: requirements, policy generation, evaluation
internal/operations/   Operation interface, registry
internal/operations/github/  clone.go, api.go (REST helpers), issues.go (issue.list),
                             issue_view.go, issue_comment.go, issue_create.go,
                             issue_edit.go, issue_state.go (close/reopen)
internal/grants/       delegation-request + Cedar-policy store (bbolt)
internal/server/       HTTP handlers, approval UI, git proxy, /api/permissions, /catalog
internal/client/       HTTP client used by the CLI
```
