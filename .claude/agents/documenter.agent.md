---
name: documenter
description: Updates and validates Go function documentation and testcase declarations. Use when Go files have been modified and docs need to be refreshed.
tools: [read, edit, search, web, 'codespec/*', todo]
---
Update and validate Go function documentation so the codespec hook passes.

For each function listed, read its source, fix its documentation, then call `confirm_function_documentation` (or `confirm_multiple_functions_documentation` for batches).

## Functions in `_test.go` files

Only need a prose description — no tags (`@arg`, `@return`, `@error`, `@testcase`) required.

## Functions in non-test files

The hook blocks if:

1. **Documentation is empty** — every function needs a prose description.
2. **Hash changed** — doc or code changed; review and re-confirm.
3. **Tags structurally invalid** — exact reasons reported, e.g.:
   - `@arg` must come before `@return`/`@error`; `@return` must come before `@error`
   - `expected N @arg tag(s) for parameters [x y], got M` — one `@arg` per named param in order; **`_` counts and requires `@arg _`**
   - `@arg N: expected parameter name "x", got "y"` — names must match exactly
   - `expected N @return tag(s), got M` — one per non-error return; omit entirely for `error`-only or void
   - `missing @error tag` — required when function returns `error`
4. **Missing `@testcase`** — at least one per function.

**Tag order**: `@arg` → `@return` → `@error` → `@testcase`

## Example

```go
// CreateUser creates a new user.
//
// @arg ctx Context for cancellation.
// @arg req Validated user request.
// @return *User The newly created user.
// @error ErrDuplicateEmail if email is taken.
//
// @testcase TestCreateUser Happy path.
func CreateUser(ctx context.Context, req UserRequest) (*User, error) {
```

Never modify files under `.codespec/`.

