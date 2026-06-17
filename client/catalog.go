package client

import (
	"context"
	"fmt"
	"net/http"

	"github.com/clems4ever/granular/resourceserver"
)

// Schemas fetches the permission schema of each requested resource server (or every configured
// resource server when none are named), returning them keyed by resource server id. The schemas are what
// an application reads to discover what each resource server can do and to build grant requests.
//
// @arg ctx Context for cancellation.
// @arg ids An optional subset of resource server ids; empty means all configured resource servers.
// @return map[string]resourceserver.Schema Each resource server id mapped to its schema.
// @error error when a resource server id is unknown or a resource server request fails.
//
// @testcase TestSchemasFiltersResourceServers fetches all resource servers and a named subset.
func (c *Client) Schemas(ctx context.Context, ids ...string) (map[string]resourceserver.Schema, error) {
	targets, err := c.resolveTargets(ids)
	if err != nil {
		return nil, err
	}
	out := make(map[string]resourceserver.Schema, len(targets))
	for _, id := range targets {
		base, _ := c.resourceServerURL(id)
		var schema resourceserver.Schema
		status, err := c.doJSON(ctx, http.MethodGet, base+"/api/schema", "", nil, &schema)
		if err != nil {
			return nil, fmt.Errorf("resource server %q schema: %w", id, err)
		}
		if status != http.StatusOK {
			return nil, fmt.Errorf("resource server %q schema: unexpected status %d", id, status)
		}
		out[id] = schema
	}
	return out, nil
}
