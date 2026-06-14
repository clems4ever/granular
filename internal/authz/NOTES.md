# authz — Cedar playground notes

A spike modelling granular, human-approved permissions as [Cedar](https://www.cedarpolicy.com/)
policies via `cedar-go`. [authz.go](authz.go) builds the entity world (the GitHub
resource hierarchy + the verb-lattice action groups); [authz_test.go](authz_test.go)
asserts how the rules we care about evaluate.

## Mapping to our model

| Our concept            | Cedar mechanism                                            |
|------------------------|------------------------------------------------------------|
| resource tree (typed)  | entities with `in` parents (`Org › Repo › Issue › Comment`) |
| verb lattice / roll-ups| **action groups** (`issue.view in issues.read in read`)    |
| matchers on the object | `when { resource.state == "open" && … }`, `like` patterns  |
| globs (`owner/*`)      | hierarchy membership (`resource in Org::"owner"`)          |
| incremental grants     | append another `permit` — policies compose, append-only    |

## Should we keep `list` and `view`, or just use `read`?

**Keep `list` and `view` as concrete actions; add `read` as a group over them.**
It isn't either/or — `read` is just a *parent* of the concrete actions, so you get
both altitudes at once:

- The agent always requests a **concrete** action (`issue.view`) — that's the real
  operation. So the leaves must exist.
- A grant can be written at **either** level: `action == issue.view` (narrow) or
  `action in read` (broad). The roll-up resolves via the action hierarchy.

See `TestReadRollupCoversListAndView` (read ⇒ list+view) and
`TestFineGrainedViewDoesNotCoverList` (view-only ⇏ list).

### The non-obvious catch that decides the shape

`list` and `view` are **not** the same verb at two scopes — they act on **different
resources**: `list` operates on the *collection* (resource = the repo), `view` on an
*item* (resource = the issue). Consequences:

1. **Don't scope `read` to the item type.** A policy like
   `action in issues.read, resource is Issue` covers `view` but silently misses
   `list` (whose resource is the repo, not an Issue) — `TestItemScopedReadMissesList`.
2. **Scope read to the repo, discriminate by action group.** `resource in Repo` +
   `action in issues.read` covers both `list` and `view`, and excludes PRs *by
   action* (`pull.view ∉ issues.read`) — `TestRepoScopedIssuesReadCoversBothAndExcludesPRs`.
   This is why per-family groups (`issues.read`, `pulls.read`) beat a single global
   `read` + a resource-type `when` filter: collection ops can't be told apart by
   resource, so the **action** has to carry the family.

### Recommended action lattice

```
issues.read   = {issue.list, issue.view}
issues.write  = {issue.create, issue.comment, issue.edit}
issues.triage = {issue.close, issue.reopen}
pulls.read    = {pull.list, pull.view}
pulls.write   = {pull.create}
read   ⊇ {issues.read, pulls.read, repo.clone, comment.read}
write  ⊇ {issues.write, pulls.write, repo.push}
```

`comment.read` sits in `read` but **not** `issues.read`, so "view issue" and "read
its comments" stay separate capabilities (`TestCommentsRequireSeparateCapability`)
— matching our `view --comments` being its own grant today.

## Other rules demonstrated

- `TestRwOnPullRequests` — "read on repo, rw on PRs" in two `permit`s.
- `TestPushOnlyToFeatureBranches` — `resource.name like "feature/*"` (matcher on the object).
- `TestReadOnlyOpenBugIssues` — attribute matchers (`state`, `labels.contains`).
- `TestOrgWideReadViaHierarchy` — `owner/*` as `resource in Org::"owner"`.
- `TestIncrementalGrantAddsWrite` — append-only widening.

## Open questions surfaced

- **Expiry**: Cedar is stateless; either pass `context.now` and add
  `unless { context.now > … }`, or keep TTL in the grant store (as today).
- **Action entities must be supplied at eval time** with their group parents
  (`seedActions`). The action lattice (and the resource entity types) are derived
  from `internal/catalog` — the single source shared by the Cedar engine, the
  `/catalog` page, and the `/api/catalog` manifest.
- **Principal identity**: modelled here as `Agent::"session"` — TBD what the real
  principal is (one agent per sandbox? per task?).
