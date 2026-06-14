# GitHub (`gh`) operations — coverage & backlog

An exhaustive map of the operations the official [`gh`](https://cli.github.com/) CLI
exposes, against what granular implements today. Use it to prioritise new operations.

## Legend

- **Status**: ✅ implemented · 🟡 in the catalog but not wired (no CLI/executor) · ⬜ not started
- **R/W**: `read` (no side effects) · `write` (mutates) · `write‑git` (mutates via the git proxy)
- **Action**: suggested id in granular's `resource.verb` scheme (for the catalog / Cedar lattice)
- **Priority**: rough importance for an agent doing real work on a repo (High / Med / Low)

## Already implemented

| `gh` command | Action | R/W |
|---|---|---|
| `gh repo clone` | `repo.clone` | read (via git proxy) ✅ |
| `git push` (via proxy) | `repo.push` | write‑git (via git proxy) ✅ |
| `gh issue list` | `issue.list` | read ✅ |
| `gh issue view` (`--comments`) | `issue.view`, `comment.read` | read ✅ |
| `gh issue create` | `issue.create` | write ✅ |
| `gh issue comment` | `issue.comment` | write ✅ |
| `gh issue edit` | `issue.edit` | write ✅ |
| `gh issue close` | `issue.close` | write ✅ |
| `gh issue reopen` | `issue.reopen` | write ✅ |
| `gh pr list` | `pull.list` | read ✅ |
| `gh pr view` (`--comments`) | `pull.view`, `comment.read` | read ✅ |
| `gh pr diff` | `pull.diff` | read ✅ |
| `gh pr create` | `pull.create` | write ✅ |
| `gh pr comment` | `pull.comment` | write ✅ |
| `gh pr review` (approve / request-changes / comment) | `pull.review` | write ✅ |
| `gh pr edit` (title / body / base) | `pull.edit` | write ✅ |
| `gh pr merge` | `pull.merge` | write ✅ |
| `gh pr close` | `pull.close` | write ✅ |
| `gh pr reopen` | `pull.reopen` | write ✅ |

> **Push** is authorised at repository granularity through the git proxy
> (`repo.push`), independently of `repo.clone`. Branch-level scoping (a
> `github.branch` matcher such as `feature/*`) would require parsing the
> receive-pack ref advertisement and is tracked as future work.
>
> **PR conversation** (`gh pr view --comments`) returns the raw issue comments,
> review comments and reviews arrays verbatim under `comments_list`,
> `review_comments_list` and `reviews_list`.

---

## Pull requests — remaining sub-commands

| `gh` command | Action | R/W | Status | Priority |
|---|---|---|---|---|
| `gh pr checkout` | `pull.checkout` | read (git proxy) | ⬜ | High |
| `gh pr ready` (mark ready / draft) | `pull.ready` | write (GraphQL) | ⬜ | Med |
| `gh pr checks` | `pull.checks` | read | ⬜ | Med |
| `gh pr status` | `pull.status` | read | ⬜ | Med |
| `gh pr update-branch` | `pull.update_branch` | write | ⬜ | Med |
| `gh pr lock` / `unlock` | `pull.lock` / `pull.unlock` | write | ⬜ | Low |

## Repository

| `gh` command | Action | R/W | Status | Priority |
|---|---|---|---|---|
| `gh repo view` (metadata, README) | `repo.view` | read | ⬜ | High |
| `gh repo create` | `repo.create` | write | ⬜ | Med |
| `gh repo fork` | `repo.fork` | write | ⬜ | Med |
| `gh repo edit` (description, topics, settings) | `repo.edit` | write | ⬜ | Med |
| `gh repo list` | `repo.list` | read | ⬜ | Med |
| `gh repo sync` (sync a fork) | `repo.sync` | write | ⬜ | Med |
| `gh repo rename` | `repo.rename` | write | ⬜ | Low |
| `gh repo archive` / `unarchive` | `repo.archive` / `repo.unarchive` | write | ⬜ | Low |
| `gh repo delete` | `repo.delete` | write (destructive) | ⬜ | Low |
| `gh repo deploy-key list/add/delete` | `repo.deploy_key.*` | read/write | ⬜ | Low |
| `gh repo gitignore` / `license` (templates) | `repo.template.read` | read | ⬜ | Low |

> **Branches** are a resource type (`github.branch`) used to scope `repo.push`
> (`name like "feature/*"`). gh has no top-level branch command; protection/rulesets
> live under `gh ruleset` / the API.

## Labels

| `gh` command | Action | R/W | Status | Priority |
|---|---|---|---|---|
| `gh label list` | `label.list` | read | ⬜ | Med |
| `gh label create` | `label.create` | write | ⬜ | Med |
| `gh label edit` | `label.edit` | write | ⬜ | Low |
| `gh label delete` | `label.delete` | write | ⬜ | Low |
| `gh label clone` (copy between repos) | `label.clone` | write | ⬜ | Low |

## Releases

| `gh` command | Action | R/W | Status | Priority |
|---|---|---|---|---|
| `gh release list` | `release.list` | read | ⬜ | Med |
| `gh release view` | `release.view` | read | ⬜ | Med |
| `gh release create` | `release.create` | write | ⬜ | Med |
| `gh release download` (assets) | `release.download` | read | ⬜ | Med |
| `gh release upload` (assets) | `release.upload` | write | ⬜ | Low |
| `gh release edit` | `release.edit` | write | ⬜ | Low |
| `gh release delete` | `release.delete` | write | ⬜ | Low |

## GitHub Actions — workflows & runs (sensitive: can execute code / mutate CI)

| `gh` command | Action | R/W | Status | Priority |
|---|---|---|---|---|
| `gh run list` | `run.list` | read | ⬜ | Med |
| `gh run view` (`--log`) | `run.view` | read | ⬜ | Med |
| `gh run watch` | `run.watch` | read | ⬜ | Med |
| `gh run download` (artifacts) | `run.download` | read | ⬜ | Med |
| `gh run rerun` | `run.rerun` | write | ⬜ | Med |
| `gh run cancel` | `run.cancel` | write | ⬜ | Med |
| `gh run delete` | `run.delete` | write | ⬜ | Low |
| `gh workflow list` | `workflow.list` | read | ⬜ | Med |
| `gh workflow view` | `workflow.view` | read | ⬜ | Low |
| `gh workflow run` (dispatch) | `workflow.run` | write (⚠ runs code) | ⬜ | Med |
| `gh workflow enable` / `disable` | `workflow.enable` / `workflow.disable` | write | ⬜ | Low |
| `gh cache list` / `delete` | `cache.list` / `cache.delete` | read/write | ⬜ | Low |

## Secrets & variables (high sensitivity)

| `gh` command | Action | R/W | Status | Priority |
|---|---|---|---|---|
| `gh secret list` (names only) | `secret.list` | read | ⬜ | Low |
| `gh secret set` | `secret.set` | write (⚠ secret material) | ⬜ | Low |
| `gh secret delete` | `secret.delete` | write | ⬜ | Low |
| `gh variable list/set/delete` | `variable.*` | read/write | ⬜ | Low |

## Search

| `gh` command | Action | R/W | Status | Priority |
|---|---|---|---|---|
| `gh search code` | `search.code` | read | ⬜ | High |
| `gh search issues` | `search.issues` | read | ⬜ | Med |
| `gh search prs` | `search.prs` | read | ⬜ | Med |
| `gh search repos` | `search.repos` | read | ⬜ | Med |
| `gh search commits` | `search.commits` | read | ⬜ | Low |

## Issues — remaining sub-commands

| `gh` command | Action | R/W | Status | Priority |
|---|---|---|---|---|
| `gh issue status` (assigned / mentioned to me) | `issue.status` | read | ⬜ | Med |
| `gh issue develop` (create a branch for an issue) | `issue.develop` | write | ⬜ | Low |
| `gh issue lock` / `unlock` | `issue.lock` / `issue.unlock` | write | ⬜ | Low |
| `gh issue pin` / `unpin` | `issue.pin` / `issue.unpin` | write | ⬜ | Low |
| `gh issue transfer` | `issue.transfer` | write | ⬜ | Low |
| `gh issue delete` | `issue.delete` | write (destructive) | ⬜ | Low |

## Gists

| `gh` command | Action | R/W | Status | Priority |
|---|---|---|---|---|
| `gh gist list` / `view` / `clone` | `gist.list` / `gist.view` / `gist.clone` | read | ⬜ | Low |
| `gh gist create` / `edit` / `rename` / `delete` | `gist.create` / `gist.edit` / `gist.rename` / `gist.delete` | write | ⬜ | Low |

## Projects (Projects v2 boards)

| `gh` command | Action | R/W | Status | Priority |
|---|---|---|---|---|
| `gh project list` / `view` | `project.list` / `project.view` | read | ⬜ | Low |
| `gh project item-list` | `project.item.list` | read | ⬜ | Low |
| `gh project create` / `edit` / `close` / `delete` / `copy` | `project.*` | write | ⬜ | Low |
| `gh project item-add` / `item-edit` / `item-archive` / `item-delete` | `project.item.*` | write | ⬜ | Low |
| `gh project field-list` / `field-create` / `field-delete` | `project.field.*` | read/write | ⬜ | Low |

## Account / org / other resources

| `gh` command | Action | R/W | Status | Priority |
|---|---|---|---|---|
| `gh status` (notifications, assignments) | `account.status` | read | ⬜ | Med |
| `gh org list` | `org.list` | read | ⬜ | Low |
| `gh ruleset list` / `view` / `check` | `ruleset.*` | read | ⬜ | Low |
| `gh ssh-key list/add/delete` | `account.ssh_key.*` | read/write (⚠) | ⬜ | Low |
| `gh gpg-key list/add/delete` | `account.gpg_key.*` | read/write (⚠) | ⬜ | Low |
| `gh codespace …` (list/create/delete/ssh/ports/logs/…) | `codespace.*` | read/write | ⬜ | Low |
| `gh attestation verify/download` | `attestation.*` | read | ⬜ | Low |
| `gh api <endpoint>` (generic REST/GraphQL) | `api.call` | read/write | ⬜ | Med (escape hatch) |

---

## Recommended next milestones

Pull requests (read + create + comment + review + edit + merge + close/reopen) and
`repo.push` are now implemented, closing the core "clone → edit → push → open PR →
review → merge" loop. The next highest-value gaps:

1. **`repo.view`** and **`search.code`** — cheap, high-utility reads for an agent
   orienting in a codebase.
2. **`pull.checkout`** — like clone, runs client-side through the proxy; lets an agent
   check out a PR branch to work on it.
3. **Branch-scoped `repo.push`** — parse the receive-pack ref advertisement so a push
   grant can be limited to a `github.branch` matcher (`feature/*`) instead of the whole
   repository.
4. **`pull.checks` / `pull.status`** — surface CI state so an agent knows whether a PR is
   mergeable before calling `pull.merge`.
5. **`pull.ready`** — toggle draft/ready (GraphQL-only, so needs a GraphQL path).

## Out of scope (local/client, not platform-resource operations)

These configure the CLI itself and don't belong in a permission broker:
`gh auth …`, `gh alias …`, `gh config …`, `gh extension …`, `gh completion`,
`gh browse`, `gh repo set-default`.
