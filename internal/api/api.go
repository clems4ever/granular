// Package api holds the wire types exchanged between the granular CLI client and
// the granular HTTP server. Keeping them in one place lets both sides depend on a
// single source of truth for the request/response shapes.
//
// An agent has two distinct verbs:
//   - Submit an Operation (POST /api/operations) — "do this now". If live Grants
//     already authorise it the server executes it immediately; otherwise the server
//     mints a pending grant request derived from the operation's requirements and
//     returns an approval URL, and a later retry of the same operation executes once
//     a human has approved.
//   - Submit a GrantRequest (POST /api/grant-requests) — "grant me these
//     capabilities for later". The server records a pending grant request for the
//     bundle; it never executes anything, it only pre-approves access.
//
// Both verbs converge on the same lifecycle: a pending request that a human
// approves, whose proposed Cedar policies become time-limited Grants.
package api

// OperationStatus is the lifecycle state reported for a grant request.
type OperationStatus string

const (
	// StatusPending means the request requires human approval that has not yet
	// been given.
	StatusPending OperationStatus = "pending"
	// StatusApproved means a human approved the grant request and live grants now
	// exist, so the CLI may retry the operation.
	StatusApproved OperationStatus = "approved"
	// StatusCompleted means the request named an operation that was authorised and
	// executed.
	StatusCompleted OperationStatus = "completed"
	// StatusRejected means a human explicitly denied the request.
	StatusRejected OperationStatus = "rejected"
	// StatusExpired means the request elapsed before being acted on.
	StatusExpired OperationStatus = "expired"
	// StatusRevoked means a previously approved grant was revoked by a human before
	// its expiry.
	StatusRevoked OperationStatus = "revoked"
)

// Operation names a concrete operation to perform now: its type id and the
// free-form parameters that configure it. An agent submits it to POST
// /api/operations and the server executes it (or requests approval first).
type Operation struct {
	Type   string         `json:"type"`
	Params map[string]any `json:"params,omitempty"`
}

// GrantRequest is an agent's explicit request, submitted to POST /api/grant-requests,
// to be granted a bundle of capabilities for later use. It names the actions and
// resources to pre-approve; unlike an Operation it never executes anything, it only
// asks a human to authorise the scope.
type GrantRequest struct {
	Reason       string       `json:"reason,omitempty"`
	Capabilities []Capability `json:"capabilities"`
}

// RequestResponse is returned by both POST /api/operations and POST
// /api/grant-requests. When Status is StatusPending the RequestID and ApprovalURL
// are populated; when StatusCompleted the Result is populated (the executed
// operation's output).
type RequestResponse struct {
	Status      OperationStatus `json:"status"`
	RequestID   string          `json:"request_id,omitempty"`
	ApprovalURL string          `json:"approval_url,omitempty"`
	Result      map[string]any  `json:"result,omitempty"`
	Error       string          `json:"error,omitempty"`
}

// RequestStatusResponse is returned by GET /api/grant-requests/{id} for inspecting
// a pending grant request's current status (whether it came from an operation that
// needed approval or from an explicit grant request).
type RequestStatusResponse struct {
	RequestID string          `json:"request_id"`
	Status    OperationStatus `json:"status"`
	Error     string          `json:"error,omitempty"`
}

// Capability grants a set of actions on resources matched by a selector. Each
// capability names a set of actions (catalog action or group names) on a resource
// selector.
type Capability struct {
	Actions  []string         `json:"actions"`
	Resource ResourceSelector `json:"resource"`
}

// ResourceSelector picks the resources a capability applies to: a catalog resource
// type plus matcher fields (e.g. {"owner":"clems4ever","name":"granular"}; a "*"
// value widens, e.g. name "*" means all repos under the owner).
type ResourceSelector struct {
	Type  string            `json:"type"`
	Match map[string]string `json:"match"`
}

// Grant describes one active (non-expired) grant — a stored Cedar policy with its
// lifetime — for listing and revocation.
type Grant struct {
	ID            string          `json:"id"`
	RequestID     string          `json:"request_id"`
	OperationType string          `json:"operation_type"`
	Description   string          `json:"description"`
	Policy        string          `json:"policy"`
	CreatedAt     string          `json:"created_at"`
	ExpiresAt     string          `json:"expires_at"`
	Status        OperationStatus `json:"status"`
}

// GrantRequestInfo describes one grant request for listing (the approval audit
// trail), without the full proposed-policy bodies.
type GrantRequestInfo struct {
	ID            string          `json:"id"`
	OperationType string          `json:"operation_type"`
	Description   string          `json:"description"`
	Status        OperationStatus `json:"status"`
	CreatedAt     string          `json:"created_at"`
}

// GrantsResponse is returned by GET /api/grants: the active grants plus the
// grant-request history.
type GrantsResponse struct {
	Grants   []Grant            `json:"grants"`
	Requests []GrantRequestInfo `json:"requests"`
}

// RevokeResponse is returned by POST /api/grants/{id}/revoke: how many active
// grants were revoked for the given id.
type RevokeResponse struct {
	Revoked int    `json:"revoked"`
	Error   string `json:"error,omitempty"`
}
