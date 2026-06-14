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

## Core concepts

- **Operation** — a concrete, parameterised action, e.g. `github.clone` with
  params `{repo, ref}`. Each operation type knows how to (a) derive a stable
  **permission key** from its parameters and (b) execute itself server-side using
  the credentials.

- **Permission key** — a deterministic string derived from the operation type and
  its parameters (e.g. `github.clone:github.com/clems4ever/granular@HEAD`). A grant
  is matched against this key, so approving a clone of repo A does not authorise a
  clone of repo B.

- **Delegation request** — created when an operation is attempted with no live
  grant. It captures the operation, a generated id, and a `pending` status. The id
  is embedded in the approval URL `…/approve/{id}`.

- **Grant** — created when a human approves a delegation request. It records the
  permission key and an expiry timestamp. An operation is allowed iff a grant for
  its key exists and `now < expiry`.

## Request flow

```
CLI                         server                         human (browser)
 |   POST /api/operations      |                                |
 |---------------------------->|  look up grant by perm-key     |
 |                             |  - none -> persist request, 202|
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
| GET    | `/api/requests/{id}`  | Inspect a delegation request's status.                        |
| GET    | `/approve/{id}`       | Human-facing approval page (HTML form).                       |
| POST   | `/approve/{id}`       | Submit approval (decision + expiry) or rejection.             |
| any    | `/git/{owner}/{repo}.git/...` | Authenticating git proxy. Re-checks the repo's grant, injects the PAT, forwards read (upload-pack) traffic to github.com. Refuses receive-pack (push). |

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
the result under the synthetic `comments_list` key. It is a **separate grant**: the
permission key gains a `+comments` suffix (`github.issue.view:owner/name#7+comments`),
so reading an issue's discussion is approved independently from its metadata, while
the CLI surface still matches `gh issue view --comments`.

## Write operations: `github.issue.comment` and `github.issue.create`

`granular github issue comment <repo> <number> --body …` posts a comment
(`gh issue comment`); `granular github issue create <repo> --title … [--body …]
[--label …] [--assignee …]` opens an issue (`gh issue create`). Both are
server-executed `POST`s and return GitHub's created object verbatim.

Because they **mutate**, two things differ from the read operations:

- **Content-scoped grants.** The permission key includes a hash of the submitted
  content — `github.issue.comment:owner/name#7:<hash(body)>`,
  `github.issue.create:owner/name:<hash(title,body,labels,assignees)>`. So the
  approver authorises *exactly* what gets written; changing the text requires a
  fresh approval. The approval page's description shows the full body/title so the
  human sees what they are signing off.
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

- **bbolt on-disk store** for grants and delegation requests (two buckets:
  `requests`, `grants`). This keeps the server stateless toward the client and lets
  approvals happen out-of-band and survive restarts. Path via `GRANULAR_DB`
  (default `<workspace>/granular.db`).
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
internal/cli/          CLI command tree, one file per command:
                         cli.go, github.go, github_clone.go, github_issue.go
internal/api/          wire types shared by client & server
internal/operations/   Operation interface, registry
internal/operations/github/  clone.go, api.go (REST helpers), issues.go (issue.list),
                             issue_view.go, issue_comment.go, issue_create.go,
                             issue_edit.go, issue_state.go (close/reopen)
internal/grants/       delegation-request + grant store (bbolt)
internal/server/       HTTP handlers, approval UI, git proxy
internal/client/       HTTP client used by the CLI
```
