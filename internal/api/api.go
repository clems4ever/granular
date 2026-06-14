// Package api holds the wire types exchanged between the granular CLI client and
// the granular HTTP server. Keeping them in one place lets both sides depend on a
// single source of truth for the request/response shapes.
package api

// OperationStatus is the lifecycle state reported for an operation attempt or a
// delegation request.
type OperationStatus string

const (
	// StatusPending means the operation requires human approval that has not yet
	// been given.
	StatusPending OperationStatus = "pending"
	// StatusApproved means a human approved the delegation request and a live grant
	// now exists, so the CLI may retry the operation.
	StatusApproved OperationStatus = "approved"
	// StatusCompleted means the operation was authorised and executed.
	StatusCompleted OperationStatus = "completed"
	// StatusRejected means a human explicitly denied the operation.
	StatusRejected OperationStatus = "rejected"
	// StatusExpired means the delegation request elapsed before being acted on.
	StatusExpired OperationStatus = "expired"
)

// OperationRequest is the body of POST /api/operations: the operation type and
// its free-form parameters.
type OperationRequest struct {
	Type   string         `json:"type"`
	Params map[string]any `json:"params"`
}

// OperationResponse is returned by POST /api/operations. When Status is
// StatusPending the RequestID and ApprovalURL are populated; when StatusCompleted
// the Result is populated.
type OperationResponse struct {
	Status      OperationStatus `json:"status"`
	RequestID   string          `json:"request_id,omitempty"`
	ApprovalURL string          `json:"approval_url,omitempty"`
	Result      map[string]any  `json:"result,omitempty"`
	Error       string          `json:"error,omitempty"`
}

// RequestStatusResponse is returned by GET /api/requests/{id} for inspecting a
// delegation request's current status (e.g. tooling or debugging).
type RequestStatusResponse struct {
	RequestID string          `json:"request_id"`
	Status    OperationStatus `json:"status"`
	Error     string          `json:"error,omitempty"`
}
