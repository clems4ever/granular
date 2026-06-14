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
  runs on the client). State (delegation requests + grants) is persisted in a
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
with the PAT and returns the issues (pull requests excluded). The grant is scoped
to the repository **and** the `--state`, so approving "open" issues does not also
authorise "closed" ones.

## Command tree

```
granular
└── github
    ├── clone <repo> <dest> [--ref]
    └── issue
        └── list <repo> [--state] [--limit]
```

## Adding an operation

1. Implement `operations.Operation` (and a matching `operations.Factory`) in a new
   package under `internal/operations/`.
2. Register it in `cmd/granular-server` with `registry.Register(type, factory)`.
3. Add a CLI sub-command in `cmd/granular` that builds the `OperationRequest`.
