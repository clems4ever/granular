package client

import (
	"context"
	"fmt"
	"net/http"

	"github.com/clems4ever/granular/gateway"
)

// Result is the outcome of a successfully authorized and executed operation.
type Result struct {
	Status string         `json:"status"`
	Result map[string]any `json:"result,omitempty"`
}

// Run submits an operation to one gateway and returns its result. The gateway asks the
// AS whether the client's policy token authorizes the operation; on an allow it executes
// and Run returns the result, and on a deny Run returns ErrNotAuthorized so the caller
// can react clearly (typically by building a grant request).
//
// @arg ctx Context for cancellation.
// @arg gatewayID The gateway to run the operation on.
// @arg op The operation type and parameters.
// @return Result The executed operation's result (on success).
// @error ErrNoToken when no policy token is configured.
// @error ErrUnknownGateway when the gateway id is not configured.
// @error ErrNotAuthorized when the AS denies the operation.
// @error error on transport failure or an unexpected gateway status.
//
// @testcase TestRunExecutesWhenAuthorized returns the result on an allow.
// @testcase TestRunNotAuthorized returns ErrNotAuthorized on a deny.
// @testcase TestRunUnknownGateway errors on an unconfigured gateway.
func (c *Client) Run(ctx context.Context, gatewayID string, op gateway.OperationRequest) (Result, error) {
	var res Result
	if c.token == "" {
		return res, ErrNoToken
	}
	base, err := c.gatewayURL(gatewayID)
	if err != nil {
		return res, err
	}
	status, err := c.doJSON(ctx, http.MethodPost, base+"/api/operations", c.token, op, &res)
	if err != nil {
		return res, err
	}
	switch status {
	case http.StatusOK:
		return res, nil
	case http.StatusForbidden:
		return res, ErrNotAuthorized
	default:
		return res, fmt.Errorf("gateway %q operation: unexpected status %d", gatewayID, status)
	}
}
