# granular

Fine-grained, time-limited, **human-approved** permissions for operations on
third-party platforms (GitHub, Google Calendar, Google Drive, …).

Instead of handing an agent a broad token, every concrete operation must be
approved by a human in a browser, and the approval **expires**. See
[DESIGN.md](DESIGN.md) for the architecture.

## Components

- **`granular`** — the CLI client. One sub-command per granular operation. Holds
  no credentials. For `github clone` it runs `git clone` **locally**, pointed at
  the server's git proxy.
- **`granular-server`** — the HTTP server. Holds the platform credentials, decides
  whether an operation is allowed, serves the approval page, and acts as a
  **credential-injecting git proxy** (it adds the PAT to git traffic but the clone
  runs on the client). State (grant requests + grants) is persisted in a
  bbolt file, so the server is stateless toward the client and approvals happen
  out-of-band.

## Build

```sh
go build -o bin/granular        ./cmd/granular
go build -o bin/granular-server ./cmd/granular-server
```

## Run the server

```sh
export GRANULAR_GITHUB_TOKEN=ghp_your_pat   # used for github.* operations
export GRANULAR_ADDR=:8080                  # listen address (default :8080)
export GRANULAR_BASE_URL=http://localhost:8080  # used to build approval links
export GRANULAR_WORKSPACE=/var/lib/granular # holds the bbolt database
# GRANULAR_DB defaults to $GRANULAR_WORKSPACE/granular.db
bin/granular-server
```

## Use the CLI

```sh
# First attempt: not yet approved -> prints an approval URL and exits.
bin/granular github clone clems4ever/granular ./granular
#  Approval required. Open this URL to approve or deny:
#    http://localhost:8080/approve/<id>
#  Once approved, re-run the same command to perform the operation.

# Open the URL, pick an expiration, click Approve. Then re-run:
bin/granular github clone clems4ever/granular ./granular
#  Authorized. Cloning clems4ever/granular into ./granular via the granular proxy...
#  Cloning into './granular'...
#  Clone completed.
```

The clone runs locally (`./granular` is created on your machine); the server only
proxies the git traffic and injects the PAT. `--server` points the CLI at a
non-default server URL. `github clone` accepts `--ref <branch-or-tag>` to control
the checked-out branch. The grant is **repo-scoped** and lasts for the expiration
chosen at approval time. Requires `git` on the client's PATH.

### List issues

```sh
bin/granular github issue list octocat/Hello-World            # open issues (default)
bin/granular github issue list octocat/Hello-World --state closed --limit 50
#  Approval required. Open this URL ...   (first time, then re-run after approving)
#  #9822  open   🚨 New article published 😫  (rididbxeuebb)
#  ...
```

Listing is **server-executed**: once approved, the server calls the GitHub API
with the PAT and returns GitHub's response verbatim (every attribute; the GitHub
issues endpoint also includes pull requests). The grant is scoped to the
repository **and** the `--state`, so approving "open" issues does not also
authorise "closed" ones.

### View an issue

```sh
bin/granular github issue view octocat/Hello-World 1
#  Approval required. Open this URL ...   (first time, then re-run after approving)
#  #1  Edited README via GitHub
#  State:    closed
#  Author:   unoju
#  ...
```

Also server-executed. The grant is scoped to the **specific issue**
(`github.issue.view:owner/name#1`), so approving one issue does not authorise
viewing another.

Add `--comments` (like `gh issue view --comments`) to also fetch the issue's
comments:

```sh
bin/granular github issue view octocat/Hello-World 1 --comments
bin/granular github issue view octocat/Hello-World 1 --comments --json | jq '.comments_list[].body'
```

`--comments` is approved as a **separate grant** (`…#1+comments`), so reading the
discussion is a distinct permission from viewing the issue's metadata. The raw
comments array is returned under the `comments_list` key.

### Comment on an issue

```sh
bin/granular github issue comment octocat/Hello-World 1 --body "Thanks, on it"
bin/granular github issue comment octocat/Hello-World 1 --body-file note.md   # "-" for stdin
```

### Create an issue

```sh
bin/granular github issue create octocat/Hello-World \
  --title "Something is broken" --body "Steps to reproduce…" \
  --label bug --label p1 --assignee octocat
```

### Edit fields / change status

```sh
bin/granular github issue edit octocat/Hello-World 1 --title "New title" --add-label bug
bin/granular github issue close octocat/Hello-World 1 --reason "not planned"
bin/granular github issue reopen octocat/Hello-World 1
```

`edit`, `close`, and `reopen` are **three separate operations/grants** — approving
a status change (`close`/`reopen`) does **not** authorise editing the issue's
fields, and vice-versa. `edit` supports `--title`, `--body`/`--body-file`,
`--add-label`/`--remove-label`, and `--add-assignee`/`--remove-assignee` (label and
assignee changes are merged against the issue's current values).

`comment`, `create`, `edit`, `close` and `reopen` are **writes**:

- Grants for content-creating writes (`comment`, `create`, `edit`) are
  **content-scoped** — the approval covers exactly the text/changes you submitted,
  so changing them requires a new approval. `close`/`reopen` grants are scoped to
  the issue. Either way the approval page shows what will happen.
- The server PAT (`GRANULAR_GITHUB_TOKEN`) must have **write** access to the repo.

### JSON output

The issue commands accept `--json` to emit GitHub's **raw** result (every
attribute, unmodified) instead of formatted text, for piping into tools like `jq`
(for `comment`/`create` this is the created object):

```sh
bin/granular github issue list octocat/Hello-World --json | jq '.[].title'
bin/granular github issue view octocat/Hello-World 1 --json | jq '{number,title,state}'
```

`--json` is a persistent flag on `github issue`, so every issue sub-command (now
and future) inherits it. It is not offered on `clone`, whose output is git's.

## Pre-approving a scoped set of permissions

Instead of approving one operation at a time, request a **bundle** up front.
Authorization is decided by [Cedar](https://www.cedarpolicy.com/) policies, so a
single broad grant covers every concrete operation it allows.

```sh
bin/granular catalog          # see resources, actions, verb groups, and the request schema

cat > req.json <<'JSON'
{ "reason": "work on granular",
  "capabilities": [
    { "actions": ["repo.clone", "issues.read", "comment.read", "pulls.read"],
      "resource": { "type": "github.repo", "match": {"owner": "clems4ever", "name": "granular"} } }
  ] }
JSON
bin/granular request -f req.json     # prints an approval URL; approve once
```

After approving, `github issue list`, `github issue view` (incl. `--comments`),
clone, etc. all work under that one grant — no per-operation prompts. A write or a
repo outside the bundle still triggers its own approval. `match` `name: "*"` widens a
capability to every repo under the owner.

The vocabulary in `catalog` (resource types, actions, verb groups like `issues.read`)
is exactly what an agent reads to build a `request`.

## Command tree

```
granular
├── catalog                       # print the capability manifest (vocabulary + request schema)
├── request -f req.json           # pre-approve a custom scoped capability bundle
└── github
    ├── clone <repo> <dest> [--ref]
    └── issue
        ├── list <repo> [--state] [--limit]
        ├── view <repo> <number> [--comments]
        ├── comment <repo> <number> --body|--body-file
        ├── create <repo> --title [--body] [--label] [--assignee]
        ├── edit <repo> <number> [--title] [--body] [--add-label] [--remove-label] [--add-assignee] [--remove-assignee]
        ├── close <repo> <number> [--reason]
        └── reopen <repo> <number>
```

## Adding an operation

1. Implement `operations.Operation` (and a matching `operations.Factory`) in a new
   package under `internal/operations/`.
2. Register it in `cmd/granular-server` with `registry.Register(type, factory)`.
3. Add a CLI sub-command in `cmd/granular` that builds an `api.Operation` and
   submits it with `client.SubmitOperation` (which wraps it in a `GrantRequest`).
