// Package proposal holds the generic, domain-agnostic wire types exchanged when a
// client asks a Gateway (RS) to sign a grant request, bundles one or more of them
// into a proposal, and submits it to the authorization server (AS) for human
// approval.
//
// The AS treats a SignedGrantRequest as opaque: it verifies the HMAC signature with
// the gateway's shared secret (which it also holds) and displays the Presentation
// verbatim, but never parses Policies. All permission meaning lives on the Gateway,
// which authored and signed both the Presentation and the Policies together so the
// client — which holds no secret — can neither tamper nor forge.
package proposal

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// GrantDetail is the structured, human-readable breakdown of one permit a grant would
// add. The consent screen shows it when the user expands a request to inspect what it
// really grants; it is index-aligned with the SignedGrantRequest's Policies, so each
// detail describes the raw Cedar policy at the same position. ResourceType is the
// gateway-supplied human name for the resource's kind (e.g. "Repository"), so the UI can
// label the otherwise-ambiguous Resource value.
type GrantDetail struct {
	Actions      []string `json:"actions,omitempty"`
	ResourceType string   `json:"resource_type,omitempty"`
	Resource     string   `json:"resource,omitempty"`
	Conditions   []string `json:"conditions,omitempty"`
}

// Presentation is the human-readable description a Gateway authors for a grant
// request. The AS displays it verbatim on the consent screen; it carries no machine
// meaning. Summary is the collapsed one-line view; Grants is the expandable per-permit
// breakdown, and the raw Policies (shown at the deepest expand) are the ground truth.
type Presentation struct {
	Title   string        `json:"title"`
	Summary string        `json:"summary"`
	Detail  string        `json:"detail,omitempty"`
	Grants  []GrantDetail `json:"grants,omitempty"`
}

// SignedGrantRequest is one Gateway-authored, Gateway-signed grant request. The HMAC
// signature covers the gateway id, the Presentation and the Policies jointly, so a
// client that relays it can neither change the displayed text without invalidating
// the policies it carries (and vice versa) nor forge a new one (it lacks the
// secret). GatewayID selects which registered secret the AS verifies against.
type SignedGrantRequest struct {
	GatewayID    string       `json:"gateway_id"`
	Presentation Presentation `json:"presentation"`
	Policies     []string     `json:"policies"`
	Signature    string       `json:"signature"` // hex HMAC-SHA256 over Canonical
}

// signedPayload is the exact, deterministic byte sequence that gets signed and
// verified: the gateway id, presentation and policies, with no signature.
type signedPayload struct {
	GatewayID    string       `json:"gateway_id"`
	Presentation Presentation `json:"presentation"`
	Policies     []string     `json:"policies"`
}

// Canonical returns the deterministic bytes signed for a grant request. Struct field
// order is fixed and there are no maps, so json.Marshal is stable across processes.
//
// @arg gatewayID The gateway identifier selecting the shared secret.
// @arg p The human-readable presentation.
// @arg policies The opaque policy texts the grant would carry.
// @return []byte The canonical bytes to sign or verify.
//
// @testcase TestSignVerifyRoundTrip signs and verifies these bytes.
func Canonical(gatewayID string, p Presentation, policies []string) []byte {
	b, _ := json.Marshal(signedPayload{GatewayID: gatewayID, Presentation: p, Policies: policies})
	return b
}

// Sign builds a SignedGrantRequest by HMAC-signing Canonical(gatewayID, p, policies)
// with the gateway's shared secret.
//
// @arg secret The gateway's shared HMAC secret.
// @arg gatewayID The gateway identifier.
// @arg p The human-readable presentation.
// @arg policies The opaque policy texts the grant would carry.
// @return SignedGrantRequest The signed grant request.
//
// @testcase TestSignVerifyRoundTrip round-trips a signed request.
func Sign(secret []byte, gatewayID string, p Presentation, policies []string) SignedGrantRequest {
	return SignedGrantRequest{
		GatewayID:    gatewayID,
		Presentation: p,
		Policies:     policies,
		Signature:    mac(secret, Canonical(gatewayID, p, policies)),
	}
}

// Verify reports whether the signature is a valid HMAC over the canonical bytes for
// the given secret. A true result proves the request was authored by a holder of the
// gateway's secret and not tampered with by the relaying client.
//
// @arg secret The gateway's shared HMAC secret (looked up by GatewayID).
// @return bool True when the signature verifies.
//
// @testcase TestSignVerifyRoundTrip verifies a freshly signed request.
// @testcase TestVerifyRejectsTamperedPresentation fails when the presentation is altered.
// @testcase TestVerifyRejectsTamperedPolicies fails when the policies are altered.
// @testcase TestVerifyRejectsWrongSecret fails under a different secret.
func (s SignedGrantRequest) Verify(secret []byte) bool {
	want := mac(secret, Canonical(s.GatewayID, s.Presentation, s.Policies))
	return hmac.Equal([]byte(s.Signature), []byte(want))
}

// mac returns the hex HMAC-SHA256 of msg under secret.
//
// @arg secret The HMAC key.
// @arg msg The message to authenticate.
// @return string The hex-encoded HMAC.
//
// @testcase TestSignVerifyRoundTrip relies on a stable MAC.
func mac(secret, msg []byte) string {
	h := hmac.New(sha256.New, secret)
	h.Write(msg)
	return hex.EncodeToString(h.Sum(nil))
}
