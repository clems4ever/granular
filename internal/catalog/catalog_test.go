package catalog

import "testing"

func TestCatalogIsConsistent(t *testing.T) {
	c := Build()

	resources := map[string]bool{}
	for _, r := range c.Resources {
		resources[r.Name] = true
	}
	for _, r := range c.Resources {
		if r.Parent != "" && !resources[r.Parent] {
			t.Errorf("resource %q references unknown parent %q", r.Name, r.Parent)
		}
	}

	groups := map[string]bool{}
	for _, g := range c.Groups {
		groups[g.Name] = true
	}
	for _, g := range c.Groups {
		for _, p := range g.Parents {
			if !groups[p] {
				t.Errorf("group %q references unknown parent %q", g.Name, p)
			}
		}
	}
	for _, a := range c.Actions {
		if !resources[a.Resource] {
			t.Errorf("action %q references unknown resource %q", a.Name, a.Resource)
		}
		for _, g := range a.Groups {
			if !groups[g] {
				t.Errorf("action %q references unknown group %q", a.Name, g)
			}
		}
	}
}

func TestResourceTreeOrder(t *testing.T) {
	rows := Build().ResourceTree()
	depth := map[string]int{}
	for _, row := range rows {
		depth[row.Resource.Name] = row.Depth
		if row.Resource.Parent != "" && row.Depth != depth[row.Resource.Parent]+1 {
			t.Errorf("%q depth %d not one below parent %q", row.Resource.Name, row.Depth, row.Resource.Parent)
		}
	}
	if depth["github.repo"] != 1 || depth["github.issue"] != 2 || depth["github.comment"] != 3 {
		t.Fatalf("unexpected depths: %v", depth)
	}
}

func TestActionLatticeCoversGroupsAndActions(t *testing.T) {
	c := Build()
	lattice := c.ActionLattice()
	for _, g := range c.Groups {
		if _, ok := lattice[g.Name]; !ok {
			t.Errorf("lattice missing group %q", g.Name)
		}
	}
	for _, a := range c.Actions {
		if _, ok := lattice[a.Name]; !ok {
			t.Errorf("lattice missing action %q", a.Name)
		}
	}
	// every referenced parent must itself be a lattice key
	for name, parents := range lattice {
		for _, p := range parents {
			if _, ok := lattice[p]; !ok {
				t.Errorf("%q references unknown parent %q", name, p)
			}
		}
	}
}

func TestVerbGroupsExpand(t *testing.T) {
	var read *GroupExpansion
	for i, g := range Build().VerbGroups() {
		if g.Group.Name == "read" {
			read = &Build().VerbGroups()[i]
		}
	}
	if read == nil {
		t.Fatal("read group missing")
	}
	has := func(name string) bool {
		for _, a := range read.Actions {
			if a.Name == name {
				return true
			}
		}
		return false
	}
	if !has("issue.view") || !has("issue.list") || !has("comment.read") || !has("repo.clone") {
		t.Fatalf("read should expand to include read actions, got %+v", read.Actions)
	}
	if has("issue.create") {
		t.Fatal("read must not include a write action")
	}
}
