package github

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/clems4ever/granular/internal/operations"
)

func TestIssueEditFactoryValidatesParams(t *testing.T) {
	if _, err := IssueEdit(map[string]any{"number": 1, "title": "t"}, operations.Env{}); !errors.Is(err, ErrMissingRepo) {
		t.Fatalf("want ErrMissingRepo, got %v", err)
	}
	if _, err := IssueEdit(map[string]any{"repo": "o/n"}, operations.Env{}); !errors.Is(err, ErrMissingIssueNumber) {
		t.Fatalf("want ErrMissingIssueNumber, got %v", err)
	}
	if _, err := IssueEdit(map[string]any{"repo": "o/n", "number": 1}, operations.Env{}); !errors.Is(err, ErrNoChanges) {
		t.Fatalf("want ErrNoChanges, got %v", err)
	}
	if _, err := IssueEdit(map[string]any{"repo": "o/n", "number": 1, "title": "t"}, operations.Env{}); err != nil {
		t.Fatalf("a title change should be valid, got %v", err)
	}
}

func TestIssueEditPermissionKeyIsContentScoped(t *testing.T) {
	a, _ := IssueEdit(map[string]any{"repo": "o/n", "number": 1, "title": "A"}, operations.Env{})
	b, _ := IssueEdit(map[string]any{"repo": "o/n", "number": 1, "title": "B"}, operations.Env{})
	if a.PermissionKey() == b.PermissionKey() {
		t.Fatalf("different edits must yield different keys")
	}
	if !strings.HasPrefix(a.PermissionKey(), "github.issue.edit:o/n#1:") {
		t.Fatalf("unexpected key %q", a.PermissionKey())
	}
}

func TestIssueEditDescribe(t *testing.T) {
	op, _ := IssueEdit(map[string]any{"repo": "o/n", "number": 3, "add_labels": []any{"bug"}}, operations.Env{})
	if d := op.Describe(); !strings.Contains(d, "o/n") || !strings.Contains(d, "#3") || !strings.Contains(d, "bug") {
		t.Fatalf("describe missing repo/number/change: %q", d)
	}
}

func TestIssueEditExecutePatches(t *testing.T) {
	var patched map[string]any
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/o/n/issues/5", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			_, _ = w.Write([]byte(`{"number":5,"labels":[{"name":"a"},{"name":"b"}],"assignees":[{"login":"x"}]}`))
		case http.MethodPatch:
			_ = json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&patched)
			_, _ = w.Write([]byte(`{"number":5,"html_url":"u"}`))
		}
	})
	stub := httptest.NewServer(mux)
	defer stub.Close()
	old := apiBaseURL
	apiBaseURL = stub.URL
	defer func() { apiBaseURL = old }()

	op, _ := IssueEdit(map[string]any{
		"repo": "o/n", "number": 5,
		"title":         "New title",
		"add_labels":    []any{"c"},
		"remove_labels": []any{"a"},
	}, operations.Env{GitHubToken: "tok"})
	if _, err := op.Execute(context.Background()); err != nil {
		t.Fatal(err)
	}
	if patched["title"] != "New title" {
		t.Fatalf("title not patched: %v", patched["title"])
	}
	// current [a,b] - remove a + add c => [b,c]
	gotLabels := toStrings(patched["labels"])
	if !reflect.DeepEqual(gotLabels, []string{"b", "c"}) {
		t.Fatalf("unexpected merged labels: %v", gotLabels)
	}
}

func TestApplyAddRemove(t *testing.T) {
	got := applyAddRemove([]string{"a", "b"}, []string{"c", "b"}, []string{"a"})
	if !reflect.DeepEqual(got, []string{"b", "c"}) {
		t.Fatalf("unexpected: %v", got)
	}
}

func toStrings(v any) []string {
	arr, _ := v.([]any)
	out := make([]string, 0, len(arr))
	for _, e := range arr {
		if s, ok := e.(string); ok {
			out = append(out, s)
		}
	}
	return out
}
