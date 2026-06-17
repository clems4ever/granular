package github

import (
	"context"
	"fmt"

	"github.com/clems4ever/granular/gateway-github/internal/authz"
	"github.com/clems4ever/granular/gateway-github/internal/operations"
)

// TypePullReview is the operation type id for submitting a review on a pull
// request.
const TypePullReview = "github.pull.review"

// PullReviewOperation submits a review (approve, request changes, or comment) on a
// GitHub pull request server-side using the server-held PAT. This is a mutating
// operation.
type PullReviewOperation struct {
	repo   string
	number int
	event  string
	body   string
	token  string
}

// PullReview builds a PullReviewOperation from request parameters and the server
// Env. It satisfies operations.Factory. Expected params: "repo" and "number"
// (required), "event" (required: approve|request_changes|comment), "body"
// (required unless the event is approve).
//
// @arg params The wire parameters carrying repo, number, event and body.
// @arg env The server Env supplying the GitHub token.
// @return operations.Operation A ready-to-execute PullReviewOperation.
// @error ErrMissingRepo if "repo" is absent or empty.
// @error ErrMissingPullNumber if "number" is absent or not positive.
// @error ErrInvalidReviewEvent if "event" is not a recognised review event.
// @error ErrMissingBody if a body is required for the event but absent.
//
// @testcase TestPullReviewFactoryValidatesParams fails on bad params or event.
// @testcase TestPullReviewRequirementsAreContentScoped builds and checks the key.
func PullReview(params map[string]any, env operations.Env) (operations.Operation, error) {
	repo := stringParam(params, "repo")
	if repo == "" {
		return nil, ErrMissingRepo
	}
	number := intParam(params, "number", 0)
	if number <= 0 {
		return nil, ErrMissingPullNumber
	}
	event, ok := normalizeReviewEvent(stringParam(params, "event"))
	if !ok {
		return nil, ErrInvalidReviewEvent
	}
	body := stringParam(params, "body")
	if body == "" && event != "APPROVE" {
		return nil, ErrMissingBody
	}
	return &PullReviewOperation{
		repo:   NormalizeRepo(repo),
		number: number,
		event:  event,
		body:   body,
		token:  env.GitHubToken,
	}, nil
}

// Type returns the github.pull.review type id.
//
// @return string The constant TypePullReview.
//
// @testcase TestPullReviewRequirementsAreContentScoped exercises a built operation.
func (o *PullReviewOperation) Type() string { return TypePullReview }

// Requirements authorizes submitting the review on the pull request, qualified by a
// hash of the exact event and body, so approving one review does not authorise
// submitting a different one.
//
// @return []authz.Requirement A single pull.review requirement, context-scoped to the event and body.
//
// @testcase TestPullReviewRequirementsAreContentScoped checks the review hash context.
func (o *PullReviewOperation) Requirements() []authz.Requirement {
	return []authz.Requirement{{
		Action:   "pull.review",
		Resource: authz.PullRef(o.repo, o.number),
		Context:  map[string]string{"review_hash": contentHash(o.event, o.body)},
	}}
}

// Describe returns a human summary for the approval page, including the review
// verdict and any comment so the approver sees exactly what will be submitted.
//
// @return string A sentence describing the review to be submitted.
//
// @testcase TestPullReviewDescribe checks the repo, number and verdict appear.
func (o *PullReviewOperation) Describe() string {
	summary := fmt.Sprintf("Submit a %q review on pull request #%d of GitHub repository %s", reviewVerb(o.event), o.number, o.repo)
	if o.body != "" {
		summary += fmt.Sprintf(":\n\n%s", o.body)
	}
	return summary
}

// Execute submits the review via the GitHub REST API and returns GitHub's created
// review object verbatim.
//
// @arg ctx Context for cancellation of the API call.
// @return map[string]any Result set to GitHub's raw created-review object.
// @error error when the request fails or GitHub returns a non-2xx status.
//
// @testcase TestPullReviewExecutePosts submits a review against a stub API.
func (o *PullReviewOperation) Execute(ctx context.Context) (map[string]any, error) {
	payload := map[string]any{"event": o.event}
	if o.body != "" {
		payload["body"] = o.body
	}
	endpoint := fmt.Sprintf("%s/repos/%s/pulls/%d/reviews", apiBaseURL, o.repo, o.number)
	var created map[string]any
	if err := postJSON(ctx, o.token, endpoint, payload, &created); err != nil {
		return nil, fmt.Errorf("submit pull request review: %w", err)
	}
	return created, nil
}

// normalizeReviewEvent maps a user-supplied review verdict to the GitHub review
// event value, reporting whether it is valid.
//
// @arg raw The raw event from the request (approve, request_changes, comment, …).
// @return string The GitHub review event value.
// @return bool True when the event is recognised.
//
// @testcase TestPullReviewFactoryValidatesParams exercises valid and invalid events.
func normalizeReviewEvent(raw string) (string, bool) {
	switch raw {
	case "approve", "APPROVE":
		return "APPROVE", true
	case "request_changes", "request-changes", "REQUEST_CHANGES":
		return "REQUEST_CHANGES", true
	case "comment", "COMMENT":
		return "COMMENT", true
	default:
		return "", false
	}
}

// reviewVerb renders a GitHub review event as a short human verb for summaries.
//
// @arg event The GitHub review event value.
// @return string A human verb such as "approve" or "request changes".
//
// @testcase TestPullReviewDescribe relies on the rendered verb.
func reviewVerb(event string) string {
	switch event {
	case "APPROVE":
		return "approve"
	case "REQUEST_CHANGES":
		return "request changes"
	default:
		return "comment"
	}
}

// ErrInvalidReviewEvent is returned when a review event is not one of approve,
// request_changes or comment.
var ErrInvalidReviewEvent = fmt.Errorf(`invalid review event (want "approve", "request_changes" or "comment")`)
