package rscli

import (
	"context"
	"fmt"

	"github.com/clems4ever/granular/internal/proposal"
	"github.com/clems4ever/granular/resourceserver"
	"github.com/spf13/cobra"
)

// requestCmd builds the `request` command: it assembles a grant request (like
// `sign`), has the resource server sign it, and submits it to the authorization
// server for human approval in one step, printing the approval URL. It is the
// single-request convenience; to bundle several grants into one approval, use
// `sign --out` per request and submit them together with `granular propose`.
//
// @return *cobra.Command The request command.
//
// @testcase TestRequestSubmitsAndPrintsURL signs and submits a request, printing the URL.
// @testcase TestRequestNeedsASURL errors clearly when no AS URL is configured.
func (a *App) requestCmd() *cobra.Command {
	var reason, resType, match, template, approver string
	var actions, binds []string
	cmd := &cobra.Command{
		Use:   "request",
		Short: "Build a grant request, sign it, and submit it to the authorization server for approval",
		RunE: func(cmd *cobra.Command, args []string) error {
			req, err := buildSignRequest(template, binds, reason, actions, resType, match)
			if err != nil {
				return err
			}
			return runRequest(context.Background(), a, req, approver)
		},
	}
	cmd.Flags().StringVar(&approver, "approver", "", "email of the person who must approve the grant (required)")
	// Template form.
	cmd.Flags().StringVar(&template, "template", "", "instantiate a resource server template instead of a freeform capability")
	cmd.Flags().StringArrayVar(&binds, "bind", nil, "template parameter, name=value (repeatable)")
	// Freeform form.
	cmd.Flags().StringVar(&reason, "reason", "", "human-readable reason shown on the consent screen")
	cmd.Flags().StringSliceVar(&actions, "actions", nil, "action or group names to request")
	cmd.Flags().StringVar(&resType, "resource", "", "resource type the capability is scoped to")
	cmd.Flags().StringVar(&match, "match", "", "resource match fields, key=value,key=value")
	_ = cmd.MarkFlagRequired("approver")
	return cmd
}

// runRequest signs the grant request against the target resource server and
// submits it to the authorization server for approver to approve, printing the
// approval URL. It requires an AS URL (from --as-url or the config's as_url).
//
// @arg ctx Context for cancellation.
// @arg a The shared App (client, AS URL, output writer).
// @arg req The grant request to sign and submit.
// @arg approver The email of the person who must approve the grant.
// @error error when no AS URL is configured, or signing/submission fails.
//
// @testcase TestRequestSubmitsAndPrintsURL prints the approval URL on success.
// @testcase TestRequestNeedsASURL errors when no AS URL is configured.
func runRequest(ctx context.Context, a *App, req resourceserver.GrantRequest, approver string) error {
	if a.asURL == "" {
		return fmt.Errorf("no authorization server URL configured; set as_url in the config or pass --as-url")
	}
	signed, err := a.c.Sign(ctx, a.RSID, req)
	if err != nil {
		return err
	}
	p, err := a.c.Submit(ctx, approver, "", []proposal.SignedGrantRequest{signed})
	if err != nil {
		return err
	}
	fmt.Fprintf(a.Out, "Grant request submitted. Ask %s to approve it:\n  %s\n", approver, p.URL)
	if p.ExpiresAt != "" {
		fmt.Fprintf(a.Out, "Expires: %s\n", p.ExpiresAt)
	}
	fmt.Fprintln(a.Out, "Once approved, re-run the operation.")
	return nil
}
