package catalog

import "testing"

// TestCatalogIsConsistent checks the built catalog only references parents, resources and groups that exist.
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
