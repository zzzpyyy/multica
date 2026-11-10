package main

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

// Fork (zzzpyyy/multica), D: role instances (plan 1.2, 3.4).

var issueAddInstancesCmd = &cobra.Command{
	Use:   "add-instances <issue-id>",
	Short: "Run several instances of one agent on an issue in parallel",
	Long: "Queue one instance of --agent per --split on the issue. Instances run beside the issue's\n" +
		"current run, each with a fresh session, its own work directory and its split; they share the\n" +
		"agent's max_concurrent_tasks. When all of them finish, the merge party is woken once.\n\n" +
		"Inside an agent task (task token) the caller must be the issue's assignee or its squad leader,\n" +
		"and --priority is refused. With a personal token you act as yourself and may set --priority.\n" +
		"Cancel one instance with `multica issue cancel-task <run-id>`.",
	Args: exactArgs(1),
	RunE: runIssueAddInstances,
}

var issueInstancesCmd = &cobra.Command{
	Use:   "instances <issue-id>",
	Short: "List an issue's instance batches with queue position and reason",
	Args:  exactArgs(1),
	RunE:  runIssueInstances,
}

func init() {
	issueAddInstancesCmd.Flags().String("agent", "", "Agent name or ID (required)")
	issueAddInstancesCmd.Flags().StringArray("split", nil, "What one instance does; repeat once per instance (required)")
	issueAddInstancesCmd.Flags().Bool("priority", false, "Put these instances ahead of the agent's other queued work (members only)")
	issueAddInstancesCmd.Flags().String("output", "table", "Output format: table or json")
	issueInstancesCmd.Flags().String("output", "table", "Output format: table or json")
	issueCmd.AddCommand(issueAddInstancesCmd)
	issueCmd.AddCommand(issueInstancesCmd)
}

func runIssueAddInstances(cmd *cobra.Command, args []string) error {
	agentInput, _ := cmd.Flags().GetString("agent")
	splits, _ := cmd.Flags().GetStringArray("split")
	priority, _ := cmd.Flags().GetBool("priority")
	if strings.TrimSpace(agentInput) == "" {
		return fmt.Errorf("--agent is required")
	}
	if len(splits) == 0 {
		return fmt.Errorf("at least one --split is required")
	}
	for i, s := range splits {
		if strings.TrimSpace(s) == "" {
			return fmt.Errorf("--split %d is empty; every instance needs a split", i+1)
		}
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	issueRef, err := resolveIssueRef(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve issue: %w", err)
	}
	agentKinds := assigneeKinds{agent: true}
	var agentID string
	if uuidRegexp.MatchString(strings.TrimSpace(agentInput)) {
		_, agentID, err = resolveAssigneeByID(ctx, client, agentInput, agentKinds)
	} else {
		_, agentID, err = resolveAssignee(ctx, client, agentInput, agentKinds)
	}
	if err != nil {
		return fmt.Errorf("resolve agent: %w", err)
	}

	var batch map[string]any
	body := map[string]any{"agent_id": agentID, "splits": splits, "priority": priority}
	if err := client.PostJSON(ctx, "/api/issues/"+url.PathEscape(issueRef.ID)+"/instances", body, &batch); err != nil {
		return fmt.Errorf("add instances: %w", err)
	}
	if out, _ := cmd.Flags().GetString("output"); out == "json" {
		return cli.PrintJSON(os.Stdout, batch)
	}
	printInstanceBatches(issueRef.ID, []map[string]any{batch})
	return nil
}

func runIssueInstances(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()
	issueRef, err := resolveIssueRef(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve issue: %w", err)
	}
	var resp struct {
		Batches []map[string]any `json:"batches"`
	}
	if err := client.GetJSON(ctx, "/api/issues/"+url.PathEscape(issueRef.ID)+"/instances", &resp); err != nil {
		return fmt.Errorf("list instances: %w", err)
	}
	if out, _ := cmd.Flags().GetString("output"); out == "json" {
		if resp.Batches == nil {
			resp.Batches = []map[string]any{}
		}
		return cli.PrintJSON(os.Stdout, resp)
	}
	printInstanceBatches(issueRef.ID, resp.Batches)
	return nil
}

// Columns per plan 3.4: batch / index / split / status / position / reason / run ID.
func printInstanceBatches(issueID string, batches []map[string]any) {
	if len(batches) == 0 {
		fmt.Fprintf(os.Stdout, "No instances on %s.\n", issueID)
		return
	}
	var rows [][]string
	for _, b := range batches {
		batchID := truncateID(strVal(b, "id"))
		total := intVal(b["total"])
		instances, _ := b["instances"].([]any)
		for _, raw := range instances {
			inst, _ := raw.(map[string]any)
			pos, reason := "", ""
			if q, ok := inst["queue"].(map[string]any); ok {
				pos = strconv.Itoa(intVal(q["position"]))
				reason = strVal(q, "reason")
				if limit, ok := q["limit"].(float64); ok {
					reason += fmt.Sprintf(" (%d/%d)", intVal(q["running"]), int(limit))
				}
			}
			rows = append(rows, []string{
				batchID,
				fmt.Sprintf("%d/%d", intVal(inst["index"]), total),
				strVal(inst, "split"),
				strVal(inst, "status"),
				pos,
				reason,
				strVal(inst, "task_id"),
			})
		}
	}
	cli.PrintTable(os.Stdout, []string{"BATCH", "INSTANCE", "SPLIT", "STATUS", "POSITION", "REASON", "RUN ID"}, rows)
}

func intVal(v any) int {
	if f, ok := v.(float64); ok {
		return int(f)
	}
	return 0
}
