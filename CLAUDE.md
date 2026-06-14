# Project context for Claude

This project uses [codespec](https://github.com/clems4ever/codespec) to keep
Go function documentation and test-case declarations in sync with the code.

## Documentation workflow

- Every Go function must carry a doc comment in the codespec format:

  ```go
  // CreateUser creates a new user and returns a pointer to the record.
  //
  // @arg ctx Context for request-scoped values and cancellation.
  // @arg req A UserRequest containing validated email and raw password.
  // @return *User The newly persisted user object.
  // @error ErrDuplicateEmail if the email is already taken.
  //
  // @testcase TestUserCreation tests that the user is created successfully.
  // @testcase TestCreateAlreadyExistingUser fails with ErrDuplicateEmail.
  ```

- When you change a function's signature or body, refresh its doc comment and
  test-case declarations in the same change.
- The `codespec check` Stop hook flags functions whose documentation drifted
  from their implementation. When it reports outdated functions, invoke the
  **documenter** agent to review and update them, then have it confirm each one
  via `confirm_function_documentation`.
- Never edit files under `.codespec/` by hand — they are managed by the
  codespec tool.
