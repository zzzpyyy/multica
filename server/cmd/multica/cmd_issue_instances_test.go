package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

// Fork (D): `multica issue add-instances` request shape and local rejects (D11).

func newAddInstancesTestCmd(agent string, splits []string, priority bool) *cobra.Command {
	cmd := &cobra.Command{Use: "add-instances"}
	cmd.Flags().String("agent", agent, "")
	cmd.Flags().StringArray("split", nil, "")
	cmd.Flags().Bool("priority", priority, "")
	cmd.Flags().String("output", "json", "")
	cmd.Flags().String("profile", "", "")
	for _, s := range splits {
		_ = cmd.Flags().Set("split", s)
	}
	return cmd
}

func TestRunIssueAddInstancesSendsSplits(t *testing.T) {
	const issueID = "11111111-1111-1111-1111-111111111111"
	const agentID = "22222222-2222-2222-2222-222222222222"
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/issues/"+issueID:
			_ = json.NewEncoder(w).Encode(map[string]any{"id": issueID, "identifier": "T-1"})
		case r.Method == http.MethodGet && r.URL.Path == "/api/agents":
			_ = json.NewEncoder(w).Encode([]map[string]any{{"id": agentID, "name": "TB"}})
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/"):
			_ = json.NewEncoder(w).Encode([]map[string]any{})
		case r.Method == http.MethodPost && r.URL.Path == "/api/issues/"+issueID+"/instances":
			_ = json.NewDecoder(r.Body).Decode(&body)
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "b1", "total": 2, "instances": []any{}})
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	setCLITestServerEnv(t, srv.URL)
	t.Setenv("MULTICA_TOKEN", "mat_test-token")

	cmd := newAddInstancesTestCmd(agentID, []string{"home page", "list page"}, false)
	if _, err := captureStdout(t, func() error { return runIssueAddInstances(cmd, []string{issueID}) }); err != nil {
		t.Fatalf("runIssueAddInstances: %v", err)
	}
	splits, _ := body["splits"].([]any)
	if body["agent_id"] != agentID || len(splits) != 2 || splits[1] != "list page" || body["priority"] != false {
		t.Fatalf("request body = %v", body)
	}
}

func TestRunIssueAddInstancesRejectsEmptySplitLocally(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("no request expected, got %s %s", r.Method, r.URL.Path)
	}))
	defer srv.Close()
	setCLITestServerEnv(t, srv.URL)
	for _, splits := range [][]string{nil, {"a", "  "}} {
		cmd := newAddInstancesTestCmd("TB", splits, false)
		if err := runIssueAddInstances(cmd, []string{"T-1"}); err == nil {
			t.Fatalf("splits %q accepted", splits)
		}
	}
}

func TestRunIssueAddInstancesShowsServerReason(t *testing.T) {
	const issueID = "11111111-1111-1111-1111-111111111111"
	const agentID = "22222222-2222-2222-2222-222222222222"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/issues/"+issueID:
			_ = json.NewEncoder(w).Encode(map[string]any{"id": issueID, "identifier": "T-1"})
		case r.Method == http.MethodGet && r.URL.Path == "/api/agents":
			_ = json.NewEncoder(w).Encode([]map[string]any{{"id": agentID, "name": "TB"}})
		case r.Method == http.MethodPost:
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(map[string]string{"code": "priority_requires_member", "error": "only a request from a user can set priority"})
		default:
			_ = json.NewEncoder(w).Encode([]map[string]any{})
		}
	}))
	defer srv.Close()
	setCLITestServerEnv(t, srv.URL)
	t.Setenv("MULTICA_TOKEN", "mat_test-token")
	err := runIssueAddInstances(newAddInstancesTestCmd(agentID, []string{"f"}, true), []string{issueID})
	if got := cli.FormatError(err, false); !strings.Contains(got, "priority_requires_member: only a request from a user can set priority") {
		t.Fatalf("message = %q", got)
	}
}
