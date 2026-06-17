package client

import (
	"context"
	"fmt"
	"net/http"

	"github.com/clems4ever/granular/resourceserver"
)

// Result is the outcome of a successfully authorized and executed operation.
type Result struct {
	Status string         `json:"status"`
	Result map[string]any `json:"result,omitempty"`
}

// Run submits an operation to one resource server and returns its result. The resource server asks the
// AS whether the client's subject token authorizes the operation; on an allow it executes
// and Run returns the result, and on a deny Run returns ErrNotAuthorized so the caller
// can react clearly (typically by building a grant request).
//
// @arg ctx Context for cancellation.
// @arg resourceServerID The resource server to run the operation on.
// @arg op The operation type and parameters.
// @return Result The executed operation's result (on success).
// @error ErrNoToken when no subject token is configured.
// @error ErrUnknownResourceServer when the resource server id is not configured.
// @error ErrNotAuthorized when the AS denies the operation.
// @error error on transport failure or an unexpected resource server status; the resource server's error body (e.g. a failed authorization check) is included when present.
//
// @testcase TestRunExecutesWhenAuthorized returns the result on an allow.
// @testcase TestRunNotAuthorized returns ErrNotAuthorized on a deny.
// @testcase TestRunUnknownResourceServer errors on an unconfigured resource server.
// @testcase TestRunSurfacesErrorBody includes the resource server's error message on an unexpected status.
func (c *Client) Run(ctx context.Context, resourceServerID string, op resourceserver.OperationRequest) (Result, error) {
	if c.token == "" {
		return Result{}, ErrNoToken
	}
	base, err := c.resourceServerURL(resourceServerID)
	if err != nil {
		return Result{}, err
	}
	// Decode the result and a possible error body together: an unexpected status
	// (e.g. 502 when the resource server can't reach the AS) carries the reason in
	// an {"error": ...} body the caller otherwise never sees.
	var res struct {
		Result
		Error string `json:"error,omitempty"`
	}
	status, err := c.doJSON(ctx, http.MethodPost, base+"/api/operations", c.token, op, &res)
	if err != nil {
		return Result{}, err
	}
	switch status {
	case http.StatusOK:
		return res.Result, nil
	case http.StatusForbidden:
		return Result{}, ErrNotAuthorized
	default:
		if res.Error != "" {
			return Result{}, fmt.Errorf("resource server %q operation: status %d: %s", resourceServerID, status, res.Error)
		}
		return Result{}, fmt.Errorf("resource server %q operation: unexpected status %d", resourceServerID, status)
	}
}
