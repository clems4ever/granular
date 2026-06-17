// Package authz holds the GitHub permission primitives an operation uses to
// declare what it needs authorized: the Requirement and ResourceRef types and the
// constructors that build a resource's identity and hierarchy. Policy generation
// and evaluation live elsewhere — the resource server SDK turns these requirements into
// Cedar policies and verify questions, and the authorization server evaluates them.
package authz

import (
	"fmt"
	"strings"
)

// ResourceRef is an operation-supplied description of a resource being acted on:
// its catalog type, its identity, optional matcher attributes, and its parent in
// the hierarchy. The resource server SDK turns it into Cedar entities.
type ResourceRef struct {
	Type   string
	ID     string
	Attrs  map[string]any
	Parent *ResourceRef
}

// Requirement is one authorization check an operation needs to pass: an action on
// a resource, optionally qualified by context (e.g. a content hash for writes).
type Requirement struct {
	Action   string
	Resource ResourceRef
	Context  map[string]string
}

// OrgRef builds a resource reference for an organization (owner).
//
// @arg owner The owner login.
// @return ResourceRef The org reference.
//
// @testcase TestRefsBuildHierarchy builds refs via these constructors.
func OrgRef(owner string) ResourceRef {
	return ResourceRef{Type: "github.org", ID: owner}
}

// RepoRef builds a resource reference for a repository, parented to its org.
//
// @arg full The "owner/name" repository.
// @return ResourceRef The repo reference.
//
// @testcase TestRefsBuildHierarchy builds a repo ref parented to its org.
func RepoRef(full string) ResourceRef {
	owner, _, _ := strings.Cut(full, "/")
	org := OrgRef(owner)
	return ResourceRef{Type: "github.repo", ID: full, Parent: &org}
}

// IssueRef builds a resource reference for an issue, parented to its repo.
//
// @arg full The "owner/name" repository.
// @arg number The issue number.
// @return ResourceRef The issue reference.
//
// @testcase TestRefsBuildHierarchy builds an issue ref parented to its repo.
func IssueRef(full string, number int) ResourceRef {
	repo := RepoRef(full)
	return ResourceRef{Type: "github.issue", ID: fmt.Sprintf("%s#%d", full, number), Parent: &repo}
}

// PullRef builds a resource reference for a pull request, parented to its repo.
//
// @arg full The "owner/name" repository.
// @arg number The pull request number.
// @return ResourceRef The pull request reference.
//
// @testcase TestRefsBuildHierarchy builds a pull ref parented to its repo.
func PullRef(full string, number int) ResourceRef {
	repo := RepoRef(full)
	return ResourceRef{Type: "github.pull", ID: fmt.Sprintf("%s#%d", full, number), Parent: &repo}
}
