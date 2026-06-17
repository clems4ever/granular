package client

import (
	"context"
	"fmt"
	"net/http"

	"github.com/clems4ever/granular/gateway"
)

// Schemas fetches the permission schema of each requested gateway (or every configured
// gateway when none are named), returning them keyed by gateway id. The schemas are what
// an application reads to discover what each gateway can do and to build grant requests.
//
// @arg ctx Context for cancellation.
// @arg ids An optional subset of gateway ids; empty means all configured gateways.
// @return map[string]gateway.Schema Each gateway id mapped to its schema.
// @error error when a gateway id is unknown or a gateway request fails.
//
// @testcase TestSchemasFiltersGateways fetches all gateways and a named subset.
func (c *Client) Schemas(ctx context.Context, ids ...string) (map[string]gateway.Schema, error) {
	targets, err := c.resolveTargets(ids)
	if err != nil {
		return nil, err
	}
	out := make(map[string]gateway.Schema, len(targets))
	for _, id := range targets {
		base, _ := c.gatewayURL(id)
		var schema gateway.Schema
		status, err := c.doJSON(ctx, http.MethodGet, base+"/api/schema", "", nil, &schema)
		if err != nil {
			return nil, fmt.Errorf("gateway %q schema: %w", id, err)
		}
		if status != http.StatusOK {
			return nil, fmt.Errorf("gateway %q schema: unexpected status %d", id, status)
		}
		out[id] = schema
	}
	return out, nil
}
