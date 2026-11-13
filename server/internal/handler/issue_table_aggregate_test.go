package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// Fork (PRD A rule 2): `filters.aggregate_sub_issues` leaves out an issue
// whose parent is itself in the list — across rows, group counts, totals and
// the status facet. Data is PRD A's P1–P4, in a project of its own so totals
// and facet counts are exact.
type aggregateFixture struct {
	project                                            string
	p1, s1, s2, s3, s4, s5, p2, p3, c1, g1, g2, p4, q1 string
}

func newAggregateFixture(t *testing.T) aggregateFixture {
	t.Helper()
	suffix := time.Now().UnixNano()
	f := aggregateFixture{project: dbfx.Project(t, fmt.Sprintf("aggregate %d", suffix))}
	issue := func(title, status, parent string, over ...testutil.Cols) string {
		cols := testutil.Cols{"status": status, "project_id": f.project}
		if parent != "" {
			cols["parent_issue_id"] = parent
		}
		for _, o := range over {
			for k, v := range o {
				cols[k] = v
			}
		}
		return dbfx.Issue(t, fmt.Sprintf("agg %s %d", title, suffix), cols)
	}
	f.p1 = issue("P1", "todo", "")
	f.s1 = issue("S1", "done", f.p1, testutil.Cols{"stage": 1})
	f.s2 = issue("S2", "cancelled", f.p1, testutil.Cols{"stage": 1})
	f.s3 = issue("S3 interface", "in_review", f.p1, testutil.Cols{"stage": 2, "assignee_type": "member", "assignee_id": testUserID})
	f.s4 = issue("S4", "blocked", f.p1, testutil.Cols{"stage": 2})
	f.s5 = issue("S5", "backlog", f.p1, testutil.Cols{"stage": 3})
	f.p2 = issue("P2", "todo", "")
	f.p3 = issue("P3", "in_progress", "")
	f.c1 = issue("C1", "todo", f.p3)
	f.g1 = issue("G1", "done", f.c1)
	f.g2 = issue("G2", "todo", f.c1)
	f.p4 = issue("P4", "todo", "")
	f.q1 = issue("Q1", "done", f.p4)
	issue("Q2", "todo", f.p4)
	return f
}

func (f aggregateFixture) query(filters issueTableFiltersRequest, search string) issueTableQuerySpec {
	return issueTableQuerySpec{
		Scope:   issueTableScope{Kind: "project", ProjectID: f.project},
		Filters: filters,
		Search:  search,
		Sort:    issueTableSortRequest{Field: "title", Direction: "asc"},
	}
}

func aggregateRows(t *testing.T, query issueTableQuerySpec, group issueTableGroupSpec, groupKey *string) issueTableRowsResponse {
	t.Helper()
	var response issueTableRowsResponse
	testutil.Call(t, testHandler.ListIssueTableRows, newRequest(http.MethodPost, "/api/issues/table/rows", issueTableRowsRequest{
		Query: query, Group: group, GroupKey: groupKey, Page: issueTablePageRequest{Limit: 100},
	})).Want(http.StatusOK).JSON(&response)
	return response
}

func rowIDs(rows issueTableRowsResponse) []string {
	ids := make([]string, 0, len(rows.Rows))
	for _, row := range rows.Rows {
		ids = append(ids, row.Issue.ID)
	}
	sort.Strings(ids)
	return ids
}

func aggIDs(ids ...string) []string {
	sort.Strings(ids)
	return ids
}

func aggregateFacets(t *testing.T, query issueTableQuerySpec, facets ...issueTableFacetSpec) issueTableFacetsResponse {
	t.Helper()
	var response issueTableFacetsResponse
	testutil.Call(t, testHandler.ListIssueTableFacets, newRequest(http.MethodPost, "/api/issues/table/facets", issueTableFacetsRequest{
		Query: query, Facets: facets,
	})).Want(http.StatusOK).JSON(&response)
	return response
}

func facetCounts(response issueTableFacetsResponse, kind string) map[string]int64 {
	counts := map[string]int64{}
	for _, facet := range response.Facets {
		if facet.Kind != kind {
			continue
		}
		for _, value := range facet.Values {
			counts[value.Key] = value.Count
		}
	}
	return counts
}

func TestIssueTableAggregateSubIssues(t *testing.T) {
	f := newAggregateFixture(t)
	aggregate := &issueTableAggregateRequest{}

	t.Run("parent in the list hides every level below it", func(t *testing.T) {
		q := f.query(issueTableFiltersRequest{AggregateSubIssues: aggregate}, "")
		rows := aggregateRows(t, q, issueTableGroupSpec{Kind: "none"}, nil)
		if got, want := rowIDs(rows), aggIDs(f.p1, f.p2, f.p3, f.p4); fmt.Sprint(got) != fmt.Sprint(want) {
			t.Fatalf("rows = %v, want P1–P4 %v", got, want)
		}
		if rows.Total != 4 {
			t.Fatalf("total = %d, want 4", rows.Total)
		}

		var groups issueTableGroupsResponse
		testutil.Call(t, testHandler.ListIssueTableGroups, newRequest(http.MethodPost, "/api/issues/table/groups", issueTableGroupsRequest{
			Query: q, Group: issueTableGroupSpec{Kind: "status"}, Page: issueTablePageRequest{Limit: 100},
		})).Want(http.StatusOK).JSON(&groups)
		counts := map[string]int64{}
		for _, group := range groups.Groups {
			counts[group.Key] = group.Count
		}
		if groups.Total != 4 || counts["status:todo"] != 3 || counts["status:in_progress"] != 1 || counts["status:in_review"] != 0 {
			t.Fatalf("groups total=%d counts=%v, want 4 with todo=3 in_progress=1", groups.Total, counts)
		}

		facets := aggregateFacets(t, q, issueTableFacetSpec{Kind: "status"})
		status := facetCounts(facets, "status")
		if facets.Total != 4 || status["todo"] != 3 || status["in_progress"] != 1 || status["in_review"] != 0 || status["done"] != 0 {
			t.Fatalf("status facet total=%d counts=%v, want 4 with todo=3 in_progress=1", facets.Total, status)
		}
	})

	t.Run("a sub-issue shows alone when its parent is filtered out", func(t *testing.T) {
		q := f.query(issueTableFiltersRequest{
			AggregateSubIssues: aggregate,
			Assignees:          []issueTableActorRef{{Type: "member", ID: testUserID}},
		}, "")
		if got, want := rowIDs(aggregateRows(t, q, issueTableGroupSpec{Kind: "none"}, nil)), aggIDs(f.s3); fmt.Sprint(got) != fmt.Sprint(want) {
			t.Fatalf("assignee=S3's: rows = %v, want S3", got)
		}
	})

	t.Run("a parent in a hidden column does not hide its sub-issues (A18)", func(t *testing.T) {
		visible := []string{"backlog", "in_progress", "in_review", "done", "blocked"} // todo hidden
		q := f.query(issueTableFiltersRequest{
			Statuses:           visible,
			AggregateSubIssues: &issueTableAggregateRequest{ParentStatuses: visible},
		}, "")
		got := rowIDs(aggregateRows(t, q, issueTableGroupSpec{Kind: "none"}, nil))
		// P1, C1 and P4 are todo, so their visible sub-issues show alone.
		if want := aggIDs(f.s1, f.s3, f.s4, f.s5, f.p3, f.g1, f.q1); fmt.Sprint(got) != fmt.Sprint(want) {
			t.Fatalf("rows = %v, want %v", got, want)
		}
	})

	t.Run("every status column's facet count equals its rows", func(t *testing.T) {
		visible := []string{"backlog", "in_progress", "in_review", "done", "blocked"}
		q := f.query(issueTableFiltersRequest{
			Statuses:           visible,
			AggregateSubIssues: &issueTableAggregateRequest{ParentStatuses: visible},
		}, "")
		status := facetCounts(aggregateFacets(t, q, issueTableFacetSpec{Kind: "status"}), "status")
		for _, s := range visible {
			key := "status:" + s
			rows := aggregateRows(t, q, issueTableGroupSpec{Kind: "status"}, &key)
			if int64(len(rows.Rows)) != status[s] {
				t.Fatalf("column %s: %d rows, status facet says %d", s, len(rows.Rows), status[s])
			}
		}
	})

	t.Run("search that only hits a sub-issue lists it", func(t *testing.T) {
		q := f.query(issueTableFiltersRequest{AggregateSubIssues: aggregate}, "interface")
		if got, want := rowIDs(aggregateRows(t, q, issueTableGroupSpec{Kind: "none"}, nil)), aggIDs(f.s3); fmt.Sprint(got) != fmt.Sprint(want) {
			t.Fatalf("search: rows = %v, want S3", got)
		}
	})

	t.Run("other facets count every matching issue", func(t *testing.T) {
		facets := []issueTableFacetSpec{{Kind: "assignee"}, {Kind: "priority"}, {Kind: "label"}}
		plain := aggregateFacets(t, f.query(issueTableFiltersRequest{}, ""), facets...)
		aggregated := aggregateFacets(t, f.query(issueTableFiltersRequest{AggregateSubIssues: aggregate}, ""), facets...)
		for _, facet := range facets {
			if a, b := facetCounts(plain, facet.Kind), facetCounts(aggregated, facet.Kind); fmt.Sprint(a) != fmt.Sprint(b) {
				t.Fatalf("%s facet: aggregated %v, plain %v", facet.Kind, b, a)
			}
		}
		if facetCounts(aggregated, "assignee")["member:"+testUserID] != 1 {
			t.Fatalf("assignee facet must still count S3: %v", facetCounts(aggregated, "assignee"))
		}
	})

	t.Run("without the field nothing changes", func(t *testing.T) {
		got := rowIDs(aggregateRows(t, f.query(issueTableFiltersRequest{}, ""), issueTableGroupSpec{Kind: "none"}, nil))
		if len(got) != 14 {
			t.Fatalf("plain query: %d rows, want all 14", len(got))
		}
	})
}

func TestIssueTableAggregateSubIssuesValidationAndCursor(t *testing.T) {
	f := newAggregateFixture(t)

	// Raw bodies: the request struct's omitempty would drop an empty list.
	for _, statuses := range [][]string{{}, {""}} {
		w := httptest.NewRecorder()
		testHandler.ListIssueTableRows(w, newRequest(http.MethodPost, "/api/issues/table/rows", map[string]any{
			"query": map[string]any{
				"scope":   map[string]any{"kind": "project", "project_id": f.project},
				"filters": map[string]any{"aggregate_sub_issues": map[string]any{"parent_statuses": statuses}},
				"sort":    map[string]any{"field": "title", "direction": "asc"},
			},
			"group":     map[string]any{"kind": "none"},
			"group_key": nil, "hierarchy": map[string]any{"enabled": false}, "parent_id": nil,
			"page": map[string]any{"limit": 10},
		}))
		if w.Code != http.StatusBadRequest {
			t.Fatalf("parent_statuses=%q: status %d, want 400: %s", statuses, w.Code, w.Body.String())
		}
	}

	// Same parent_statuses in another order share the cursor.
	first := f.query(issueTableFiltersRequest{AggregateSubIssues: &issueTableAggregateRequest{ParentStatuses: []string{"todo", "in_progress"}}}, "")
	second := f.query(issueTableFiltersRequest{AggregateSubIssues: &issueTableAggregateRequest{ParentStatuses: []string{"in_progress", "todo", "todo"}}}, "")
	var page issueTableRowsResponse
	testutil.Call(t, testHandler.ListIssueTableRows, newRequest(http.MethodPost, "/api/issues/table/rows", issueTableRowsRequest{
		Query: first, Group: issueTableGroupSpec{Kind: "none"}, Page: issueTablePageRequest{Limit: 1},
	})).Want(http.StatusOK).JSON(&page)
	if page.NextCursor == nil {
		t.Fatal("expected a next cursor")
	}
	w := httptest.NewRecorder()
	testHandler.ListIssueTableRows(w, newRequest(http.MethodPost, "/api/issues/table/rows", issueTableRowsRequest{
		Query: second, Group: issueTableGroupSpec{Kind: "none"}, Page: issueTablePageRequest{Limit: 1, Cursor: page.NextCursor},
	}))
	if w.Code != http.StatusOK {
		t.Fatalf("reordered parent_statuses rejected the cursor: %d %s", w.Code, w.Body.String())
	}
	var next issueTableRowsResponse
	if err := json.NewDecoder(w.Body).Decode(&next); err != nil || len(next.Rows) != 1 || next.Rows[0].Issue.ID == page.Rows[0].Issue.ID {
		t.Fatalf("second page = %+v (err %v), want the next row", next.Rows, err)
	}
}
