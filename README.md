# granular

Fine-grained, time-limited, **human-approved** permissions for operations on
third-party platforms (GitHub, and others later).

Instead of handing an agent a broad token, every permission is **frozen into a
grant request by the platform gateway, approved by a human in a browser, and
expires**. The authorization server that runs the consent screen is fully
domain-agnostic: it never sees a platform credential and understands no
permission vocabulary. See [DESIGN.md](DESIGN.md) for the architecture.

## Components

granular is four binaries:

- **`granular-client`** — the agent CLI. Reads a gateway's permission schema,
  builds grant requests (freeform or from a template), submits them to the
  authorization server for approval, and runs operations once they are
  authorized. Holds **no** platform credential and **no** signing secret.
- **`granular-github-gateway`** — the GitHub **gateway** (Resource Server). Owns
  the GitHub credential and the permission vocabulary (resources, actions,
  templates, operations). It **signs** each grant request — freezing the exact
  human-readable consent text and the machine-enforced policy — and executes an
  operation only after the AS confirms it is authorized. The gateway logic is the
  generic `gateway` SDK; this binary wires the GitHub implementation into it.
- **`granular-auth-server`** — the **authorization server** (AS): the generic
  policy authority. It stores grants, runs the human consent screen (GitHub
  login, gated on the approver's email), and answers allow/deny. It holds **no**
  platform credential and renders the gateway-signed consent text **verbatim** —
  it cannot interpret or add to it.
- **`granular-policy`** — the admin CLI for the **policy token** lifecycle. An
  administrator mints a token against the AS admin credential, hands it to a
  client, and can later inspect or destroy it.

## How it works

```
admin            client (agent)              gateway (GitHub)         AS + human
  |  mint policy token ----------------------------------------------> |
  |<------------------ token --------------------------------------- (PUT /api/policy)
            | catalog / template (GET /api/schema) -----> |
            | sign grant request -----------------------> | freeze consent text
            |<----- signed (presentation + policy) ------ | + Cedar policy, HMAC-sign
            | propose (POST /api/proposals) -------------------------> | store, render
            |                                                          | consent VERBATIM
            |                                          human approves ->| grant attached
            | op (POST /api/operations) --> | verify (POST /api/verify)>| allow?
            |<--------- result ------------ | execute with credential   |
```

1. **Mint a policy token** (admin): `granular-policy create`. The token is the
   bearer credential the client attaches grant requests and operations to.
2. **Explore** (client): `granular catalog` / `granular template` print what a
   gateway can grant.
3. **Sign** (client → gateway): the gateway freezes the request into a
   *presentation* (the human-readable consent text) plus a *policy* (the
   machine-enforced Cedar rule) and HMAC-signs them.
4. **Propose** (client → AS): the signed request is packed into a proposal and
   submitted; the AS returns an approval URL and the request **expires** if not
   acted on.
5. **Approve** (human): open the URL, log in with GitHub (only the human whose
   verified email matches the named approver may decide), pick how long the grant
   lasts, approve. The grant attaches to the policy token with a TTL.
6. **Run** (client → gateway → AS): `granular op …` calls the gateway, which asks
   the AS to verify the policy token authorizes it before executing with the
   GitHub credential.

## Build

```sh
make build                 # builds all four binaries into ./bin
# or individually:
go build -o bin/granular-client         ./cmd/granular-client
go build -o bin/granular-auth-server    ./cmd/granular-auth-server
go build -o bin/granular-github-gateway ./cmd/granular-github-gateway
go build -o bin/granular-policy         ./cmd/granular-policy
```

## Configure and run

Each binary is configured by a YAML file (copy the matching `*.example.yaml`).
**Secrets are never inline** — every `*_file` key names a path to a file holding
the secret, read at load time.

```sh
# 1. Authorization server (the policy authority + consent UI), listens on :9090
cp granular-auth.example.yaml granular-auth.yaml && $EDITOR granular-auth.yaml
bin/granular-auth-server                          # --config to override the path

# 2. GitHub gateway (holds the PAT + vocabulary), listens on :8080
cp granular-github-gateway.example.yaml granular-github-gateway.yaml && $EDITOR ...
bin/granular-github-gateway

# 3. Client
cp granular-client.example.yaml granular-client.yaml && $EDITOR granular-client.yaml
```

The gateway and the AS share a **per-gateway HMAC secret** (`secret_file` on each
side, under the same `gateway_id`); the gateway signs grant requests with it and
the AS verifies them. The AS's policy-administration endpoints are gated by an
**admin token** (`admin_token_file`); when unset, policy administration is
disabled (fail closed). The consent pages can require a **GitHub login**
(`auth.client_id` + `auth.client_secret_file`); each proposal names its approver
and only that verified email may decide it.

## Use the client

```sh
# Mint a policy token (admin, against the AS admin token) and give it to the client.
bin/granular-policy create --admin-token-file admin.token
#   -> prints a policy token; put its path in granular-client.yaml's token_file

# Explore what the gateway can grant.
bin/granular catalog
bin/granular template                          # list templates
bin/granular template read-repo                # what a template grants

# Build a grant request and have the gateway sign it — from a template …
bin/granular sign --gateway github-gateway \
  --template read-repo --bind repo=clems4ever/granular --out req.json

# … or freeform from raw actions + a scoped resource.
bin/granular sign --gateway github-gateway \
  --reason "work on granular" \
  --actions repo.read,issues.read \
  --resource github.repo --match owner=clems4ever,name=granular --out req.json

# Submit signed requests as one proposal for approval.
bin/granular propose req.json --approver you@example.com
#   -> prints an approval URL; open it, log in, pick a duration, approve.

# Once approved, run operations under the policy token.
bin/granular op github-gateway repo.clone -p repo=clems4ever/granular
```

Observe active grants and the request history in the AS web UI at `/activity`.

## Command trees

```
granular (granular-client)
├── catalog [gateway-id ...] [--json]      # print a gateway's permission schema
├── template [name] [--gateway]            # list templates, or detail one
├── op <gateway-id> <type> [-p k=v ...]    # run an operation (executes when authorized)
├── sign --gateway <id> [--out f]          # freeze a grant request via the gateway
│     ├── --template <name> --bind k=v     #   from a template
│     └── --reason --actions --resource --match   # or freeform
└── propose <signed-file ...> --approver <email>   # submit a proposal for approval

granular-policy                            # admin: --admin-token[-file]
├── create                                 # mint a policy token
├── show <policy-token>                    # inspect a token's grants
└── destroy <policy-token>                 # revoke a token and its grants
```

## Adding a gateway or operation

- **A new platform gateway** implements the `gateway.Schema` (resources, actions,
  templates, operations) and the operation executors, then wires them into the
  generic `gateway` SDK in a new `cmd/granular-<platform>-gateway`. See
  `gateway-github/` for the GitHub reference implementation.
- **A new GitHub operation** implements `operations.Operation` under
  `gateway-github/internal/operations/github/`, is registered in the gateway's
  schema, and is invoked with `granular op github-gateway <type>`.

## Repository layout

```
cmd/                       the four binary entrypoints (main.go + tests only)
clientcli/                 client CLI command tree (catalog, template, op, sign, propose)
client/                    client SDK (proposals, operations, policy admin)
gateway/                   generic gateway SDK (schema, sign, present, verify, asclient)
gateway-github/            GitHub gateway implementation (schema, templates, operations)
gateway-github/internal/   GitHub-only concerns, unimportable from outside the gateway:
  catalog/                   GitHub permission vocabulary (resources, actions)
  authz/                     Cedar GitHub entity world + capability→policy
  operations/                operation framework + GitHub operation implementations
auth_server/               authorization server: config, store (bbolt), HTTP + consent UI
internal/proposal/         the signed (presentation + policy) artifact shared on the wire
internal/verify/           generic, domain-agnostic gateway↔AS verify wire types
internal/api/              shared wire types
```

See [DESIGN.md](DESIGN.md) for the full architecture, HTTP API, and security model.
