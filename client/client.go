// Package client is the SDK for talking to one or more granular gateways and the
// authorization server (AS) over HTTP. It is the client-side counterpart to the gateway
// SDK: an application configures it with the gateways it knows about and the AS, then
// uses a single Client to catalog the gateways' permission schemas, run operations
// (executed when the policy authorizes them, a clear error otherwise), and assemble
// grant requests — the most interesting abstraction, where the client builds per-gateway
// capability requests from the discovered schemas, has each gateway sign its own, packs
// the signed requests into a proposal, and submits it to the AS for human consent.
//
// The SDK is deliberately small and free of any platform vocabulary or configuration
// format; concrete implementations (such as the CLI) supply those.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Gateway is one configured gateway: a stable id and the base URL it is reached at.
type Gateway struct {
	ID      string
	BaseURL string
}

// Config configures a Client: the AS base URL, an optional policy token (the bearer
// credential proposals and policy reads are made under), and the known gateways.
type Config struct {
	ASURL    string
	Token    string
	Gateways []Gateway
}

// Client talks to the configured gateways and AS over HTTP.
type Client struct {
	asURL    string
	token    string
	gateways map[string]string // id -> base URL
	order    []string          // gateway ids in configuration order
	http     *http.Client
}

// Sentinel errors callers can match with errors.Is.
var (
	// ErrNotAuthorized is returned by Run when the AS denies the operation.
	ErrNotAuthorized = errors.New("operation not authorized by policy")
	// ErrNoToken is returned when an operation needs a policy token but none is set.
	ErrNoToken = errors.New("no policy token configured")
	// ErrUnknownGateway is returned when a referenced gateway id is not configured.
	ErrUnknownGateway = errors.New("unknown gateway")
)

// New creates a Client from its configuration.
//
// @arg cfg The client configuration (AS URL, token, gateways).
// @return *Client A ready client with a default timeout.
//
// @testcase TestSchemasFiltersGateways constructs a client over stub gateways.
func New(cfg Config) *Client {
	c := &Client{
		asURL:    trimSlash(cfg.ASURL),
		token:    cfg.Token,
		gateways: make(map[string]string, len(cfg.Gateways)),
		http:     &http.Client{Timeout: 5 * time.Minute},
	}
	for _, g := range cfg.Gateways {
		c.gateways[g.ID] = trimSlash(g.BaseURL)
		c.order = append(c.order, g.ID)
	}
	return c
}

// GatewayIDs returns the configured gateway ids in configuration order.
//
// @return []string The configured gateway ids.
//
// @testcase TestSchemasFiltersGateways lists the configured gateways.
func (c *Client) GatewayIDs() []string {
	out := make([]string, len(c.order))
	copy(out, c.order)
	return out
}

// gatewayURL returns the base URL configured for a gateway id.
//
// @arg id The gateway id.
// @return string The gateway's base URL.
// @error ErrUnknownGateway when the id is not configured.
//
// @testcase TestRunUnknownGateway errors on an unconfigured gateway.
func (c *Client) gatewayURL(id string) (string, error) {
	base, ok := c.gateways[id]
	if !ok {
		return "", fmt.Errorf("%w: %q", ErrUnknownGateway, id)
	}
	return base, nil
}

// resolveTargets returns the requested gateway ids, or all configured ids (in order)
// when none are requested, erroring if any requested id is unknown.
//
// @arg ids The requested subset, or empty for all.
// @return []string The resolved gateway ids.
// @error ErrUnknownGateway when a requested id is not configured.
//
// @testcase TestSchemasFiltersGateways resolves a subset and the full set.
func (c *Client) resolveTargets(ids []string) ([]string, error) {
	if len(ids) == 0 {
		return c.GatewayIDs(), nil
	}
	for _, id := range ids {
		if _, ok := c.gateways[id]; !ok {
			return nil, fmt.Errorf("%w: %q", ErrUnknownGateway, id)
		}
	}
	return ids, nil
}

// doJSON performs an HTTP request with an optional JSON body and bearer token, decoding
// a JSON response into out (when non-nil) and returning the status code. It does not
// treat any status as an error; callers interpret the status.
//
// @arg ctx Context for cancellation.
// @arg method The HTTP method.
// @arg url The absolute request URL.
// @arg bearer A bearer token to send, or "" for none.
// @arg body The value to marshal as the JSON request body, or nil for none.
// @arg out A pointer to decode the JSON response into, or nil to discard it.
// @return int The HTTP status code.
// @error error on transport failure, request construction, or a body decode error.
//
// @testcase TestCreatePolicySetsToken drives a PUT through doJSON.
func (c *Client) doJSON(ctx context.Context, method, url, bearer string, body, out any) (int, error) {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return 0, err
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, reader)
	if err != nil {
		return 0, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if out == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return resp.StatusCode, nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return resp.StatusCode, fmt.Errorf("decode response (status %d): %w", resp.StatusCode, err)
	}
	return resp.StatusCode, nil
}

// trimSlash removes a single trailing slash from a base URL.
//
// @arg s The URL.
// @return string The URL without a trailing slash.
//
// @testcase TestSchemasFiltersGateways relies on normalised base URLs.
func trimSlash(s string) string {
	if len(s) > 0 && s[len(s)-1] == '/' {
		return s[:len(s)-1]
	}
	return s
}
