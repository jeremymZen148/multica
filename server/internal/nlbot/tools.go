package nlbot

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// toolDef is a provider-agnostic tool definition.
type toolDef struct {
	Name        string
	Description string
	// Parameters is a JSON Schema object describing the tool's input.
	Parameters map[string]any
}

func buildToolDefs() []toolDef {
	return []toolDef{
		{
			Name:        "list_issues",
			Description: "List issues in this workspace. Returns title, status, priority, and UUID for each issue.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"status": map[string]any{
						"type":        "string",
						"description": "Filter by status. One of: todo, in_progress, in_review, done. Omit to return all.",
						"enum":        []string{"todo", "in_progress", "in_review", "done"},
					},
					"priority": map[string]any{
						"type":        "string",
						"description": "Filter by priority. One of: urgent, high, medium, low, no_priority. Omit to return all.",
						"enum":        []string{"urgent", "high", "medium", "low", "no_priority"},
					},
					"limit": map[string]any{
						"type":        "integer",
						"description": "Maximum number of issues to return (default 10, max 50).",
					},
				},
				"required": []string{},
			},
		},
		{
			Name:        "get_issue",
			Description: "Get details of a specific issue by its UUID.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"issue_id": map[string]any{
						"type":        "string",
						"description": "UUID of the issue.",
					},
				},
				"required": []string{"issue_id"},
			},
		},
		{
			Name:        "set_issue_status",
			Description: "Change the status of an issue.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"issue_id": map[string]any{
						"type":        "string",
						"description": "UUID of the issue.",
					},
					"status": map[string]any{
						"type":        "string",
						"description": "New status.",
						"enum":        []string{"todo", "in_progress", "in_review", "done"},
					},
				},
				"required": []string{"issue_id", "status"},
			},
		},
		{
			Name:        "add_comment",
			Description: "Add a comment to an issue.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"issue_id": map[string]any{
						"type":        "string",
						"description": "UUID of the issue.",
					},
					"text": map[string]any{
						"type":        "string",
						"description": "Comment text.",
					},
				},
				"required": []string{"issue_id", "text"},
			},
		},
		{
			Name:        "create_issue",
			Description: "Create a new issue in this workspace.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"title": map[string]any{
						"type":        "string",
						"description": "Issue title.",
					},
					"description": map[string]any{
						"type":        "string",
						"description": "Optional longer description.",
					},
					"status": map[string]any{
						"type":        "string",
						"description": "Initial status (default: todo).",
						"enum":        []string{"todo", "in_progress", "in_review", "done"},
					},
					"priority": map[string]any{
						"type":        "string",
						"description": "Priority (default: medium).",
						"enum":        []string{"urgent", "high", "medium", "low", "no_priority"},
					},
				},
				"required": []string{"title"},
			},
		},
		{
			Name:        "set_issue_priority",
			Description: "Change the priority of an issue.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"issue_id": map[string]any{
						"type":        "string",
						"description": "UUID of the issue.",
					},
					"priority": map[string]any{
						"type":        "string",
						"description": "New priority.",
						"enum":        []string{"urgent", "high", "medium", "low", "no_priority"},
					},
				},
				"required": []string{"issue_id", "priority"},
			},
		},
		{
			Name:        "list_members",
			Description: "List workspace members. Returns name, email, and user UUID for each member. Use this to find the right user UUID before calling assign_issue.",
			Parameters: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
				"required":   []string{},
			},
		},
		{
			Name:        "list_agents",
			Description: "List AI agents in this workspace. Returns name, status, and agent UUID. Use this to find the right agent UUID before calling assign_issue.",
			Parameters: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
				"required":   []string{},
			},
		},
		{
			Name:        "assign_issue",
			Description: "Assign an issue to a member or agent. Call list_members or list_agents first to get the assignee UUID.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"issue_id": map[string]any{
						"type":        "string",
						"description": "UUID of the issue to assign.",
					},
					"assignee_type": map[string]any{
						"type":        "string",
						"description": "Whether the assignee is a human member or an AI agent.",
						"enum":        []string{"member", "agent"},
					},
					"assignee_id": map[string]any{
						"type":        "string",
						"description": "UUID of the member (user UUID from list_members) or agent (agent UUID from list_agents).",
					},
				},
				"required": []string{"issue_id", "assignee_type", "assignee_id"},
			},
		},
		{
			Name:        "trigger_agent",
			Description: "Start an agent run on an issue. The issue must already have an agent assigned (use assign_issue with assignee_type=agent first). Returns the queued task ID.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"issue_id": map[string]any{
						"type":        "string",
						"description": "UUID of the issue to run the agent on.",
					},
				},
				"required": []string{"issue_id"},
			},
		},
	}
}

// executeTool dispatches a named tool call to the appropriate DB operation.
func executeTool(ctx context.Context, queries *db.Queries, enqueuer TaskEnqueuer, wsID, userID pgtype.UUID, name string, args map[string]any) (string, error) {
	switch name {
	case "list_issues":
		return toolListIssues(ctx, queries, wsID, args)
	case "get_issue":
		return toolGetIssue(ctx, queries, wsID, args)
	case "set_issue_status":
		return toolSetIssueStatus(ctx, queries, wsID, args)
	case "set_issue_priority":
		return toolSetIssuePriority(ctx, queries, wsID, args)
	case "add_comment":
		return toolAddComment(ctx, queries, wsID, userID, args)
	case "create_issue":
		return toolCreateIssue(ctx, queries, wsID, userID, args)
	case "list_members":
		return toolListMembers(ctx, queries, wsID)
	case "list_agents":
		return toolListAgents(ctx, queries, wsID)
	case "assign_issue":
		return toolAssignIssue(ctx, queries, wsID, args)
	case "trigger_agent":
		return toolTriggerAgent(ctx, queries, enqueuer, wsID, args)
	default:
		return "", fmt.Errorf("unknown tool: %s", name)
	}
}

func toolListIssues(ctx context.Context, queries *db.Queries, wsID pgtype.UUID, args map[string]any) (string, error) {
	limit := int32(10)
	if v, ok := args["limit"]; ok {
		switch n := v.(type) {
		case float64:
			limit = int32(n)
		case int:
			limit = int32(n)
		}
	}
	if limit > 50 {
		limit = 50
	}

	params := db.ListIssuesParams{
		WorkspaceID: wsID,
		Limit:       limit,
		Offset:      0,
	}
	if statusVal, ok := args["status"].(string); ok && statusVal != "" {
		params.Status = pgtype.Text{String: statusVal, Valid: true}
	}
	if priorityVal, ok := args["priority"].(string); ok && priorityVal != "" {
		params.Priority = pgtype.Text{String: priorityVal, Valid: true}
	}

	rows, err := queries.ListIssues(ctx, params)
	if err != nil {
		return "", fmt.Errorf("list_issues: %w", err)
	}

	type issueItem struct {
		ID       string `json:"id"`
		Title    string `json:"title"`
		Status   string `json:"status"`
		Priority string `json:"priority"`
	}
	items := make([]issueItem, 0, len(rows))
	for _, r := range rows {
		items = append(items, issueItem{
			ID:       uuidStr(r.ID),
			Title:    r.Title,
			Status:   r.Status,
			Priority: r.Priority,
		})
	}
	b, _ := json.Marshal(items)
	return string(b), nil
}

func toolGetIssue(ctx context.Context, queries *db.Queries, wsID pgtype.UUID, args map[string]any) (string, error) {
	issueIDStr, _ := args["issue_id"].(string)
	if issueIDStr == "" {
		return "", fmt.Errorf("get_issue: issue_id is required")
	}
	issueUUID, err := parseUUID(issueIDStr)
	if err != nil {
		return "", fmt.Errorf("get_issue: invalid issue_id: %w", err)
	}

	issue, err := queries.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{
		ID:          issueUUID,
		WorkspaceID: wsID,
	})
	if err == pgx.ErrNoRows {
		return "", fmt.Errorf("issue not found")
	}
	if err != nil {
		return "", fmt.Errorf("get_issue: %w", err)
	}

	result := map[string]any{
		"id":       uuidStr(issue.ID),
		"title":    issue.Title,
		"status":   issue.Status,
		"priority": issue.Priority,
	}
	if issue.Description.Valid {
		result["description"] = issue.Description.String
	}
	b, _ := json.Marshal(result)
	return string(b), nil
}

func toolSetIssueStatus(ctx context.Context, queries *db.Queries, wsID pgtype.UUID, args map[string]any) (string, error) {
	issueIDStr, _ := args["issue_id"].(string)
	status, _ := args["status"].(string)
	if issueIDStr == "" || status == "" {
		return "", fmt.Errorf("set_issue_status: issue_id and status are required")
	}
	if !validStatus(status) {
		return "", fmt.Errorf("set_issue_status: invalid status %q", status)
	}
	issueUUID, err := parseUUID(issueIDStr)
	if err != nil {
		return "", fmt.Errorf("set_issue_status: invalid issue_id: %w", err)
	}

	// Verify the issue belongs to this workspace.
	if _, err := queries.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{
		ID:          issueUUID,
		WorkspaceID: wsID,
	}); err == pgx.ErrNoRows {
		return "", fmt.Errorf("issue not found")
	} else if err != nil {
		return "", fmt.Errorf("set_issue_status: %w", err)
	}

	updated, err := queries.UpdateIssueStatus(ctx, db.UpdateIssueStatusParams{
		ID:     issueUUID,
		Status: status,
	})
	if err != nil {
		return "", fmt.Errorf("set_issue_status: %w", err)
	}
	return fmt.Sprintf(`{"id":%q,"title":%q,"status":%q}`, uuidStr(updated.ID), updated.Title, updated.Status), nil
}

func toolAddComment(ctx context.Context, queries *db.Queries, wsID, userID pgtype.UUID, args map[string]any) (string, error) {
	issueIDStr, _ := args["issue_id"].(string)
	text, _ := args["text"].(string)
	if issueIDStr == "" || text == "" {
		return "", fmt.Errorf("add_comment: issue_id and text are required")
	}
	issueUUID, err := parseUUID(issueIDStr)
	if err != nil {
		return "", fmt.Errorf("add_comment: invalid issue_id: %w", err)
	}

	// Verify workspace ownership.
	if _, err := queries.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{
		ID:          issueUUID,
		WorkspaceID: wsID,
	}); err == pgx.ErrNoRows {
		return "", fmt.Errorf("issue not found")
	} else if err != nil {
		return "", fmt.Errorf("add_comment: %w", err)
	}

	_, err = queries.CreateComment(ctx, db.CreateCommentParams{
		IssueID:     issueUUID,
		WorkspaceID: wsID,
		AuthorType:  "member",
		AuthorID:    userID,
		Content:     text,
		Type:        "comment",
	})
	if err != nil {
		return "", fmt.Errorf("add_comment: %w", err)
	}
	return `{"ok":true}`, nil
}

func toolCreateIssue(ctx context.Context, queries *db.Queries, wsID, userID pgtype.UUID, args map[string]any) (string, error) {
	title, _ := args["title"].(string)
	if title == "" {
		return "", fmt.Errorf("create_issue: title is required")
	}

	status := "todo"
	if s, ok := args["status"].(string); ok && validStatus(s) {
		status = s
	}

	priority := "medium"
	if p, ok := args["priority"].(string); ok && validPriority(p) {
		priority = p
	}

	var desc pgtype.Text
	if d, ok := args["description"].(string); ok && d != "" {
		desc = pgtype.Text{String: d, Valid: true}
	}

	// Get the next issue number atomically.
	number, err := queries.IncrementIssueCounter(ctx, wsID)
	if err != nil {
		return "", fmt.Errorf("create_issue: increment counter: %w", err)
	}

	issue, err := queries.CreateIssue(ctx, db.CreateIssueParams{
		WorkspaceID: wsID,
		Title:       title,
		Description: desc,
		Status:      status,
		Priority:    priority,
		CreatorType: "member",
		CreatorID:   userID,
		Number:      number,
		Position:    float64(number) * 1000,
	})
	if err != nil {
		return "", fmt.Errorf("create_issue: %w", err)
	}
	return fmt.Sprintf(`{"id":%q,"title":%q,"status":%q,"priority":%q,"number":%d}`, uuidStr(issue.ID), issue.Title, issue.Status, issue.Priority, issue.Number), nil
}

func toolSetIssuePriority(ctx context.Context, queries *db.Queries, wsID pgtype.UUID, args map[string]any) (string, error) {
	issueIDStr, _ := args["issue_id"].(string)
	priority, _ := args["priority"].(string)
	if issueIDStr == "" || priority == "" {
		return "", fmt.Errorf("set_issue_priority: issue_id and priority are required")
	}
	if !validPriority(priority) {
		return "", fmt.Errorf("set_issue_priority: invalid priority %q", priority)
	}
	issueUUID, err := parseUUID(issueIDStr)
	if err != nil {
		return "", fmt.Errorf("set_issue_priority: invalid issue_id: %w", err)
	}

	if _, err := queries.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{
		ID:          issueUUID,
		WorkspaceID: wsID,
	}); err == pgx.ErrNoRows {
		return "", fmt.Errorf("issue not found")
	} else if err != nil {
		return "", fmt.Errorf("set_issue_priority: %w", err)
	}

	updated, err := queries.UpdateIssuePriority(ctx, db.UpdateIssuePriorityParams{
		ID:       issueUUID,
		Priority: priority,
	})
	if err != nil {
		return "", fmt.Errorf("set_issue_priority: %w", err)
	}
	return fmt.Sprintf(`{"id":%q,"title":%q,"priority":%q}`, uuidStr(updated.ID), updated.Title, updated.Priority), nil
}

func toolListMembers(ctx context.Context, queries *db.Queries, wsID pgtype.UUID) (string, error) {
	rows, err := queries.ListMembersWithUser(ctx, wsID)
	if err != nil {
		return "", fmt.Errorf("list_members: %w", err)
	}

	type memberItem struct {
		UserID string `json:"user_id"`
		Name   string `json:"name"`
		Email  string `json:"email"`
	}
	items := make([]memberItem, 0, len(rows))
	for _, r := range rows {
		items = append(items, memberItem{
			UserID: uuidStr(r.UserID),
			Name:   r.UserName,
			Email:  r.UserEmail,
		})
	}
	b, _ := json.Marshal(items)
	return string(b), nil
}

func toolListAgents(ctx context.Context, queries *db.Queries, wsID pgtype.UUID) (string, error) {
	rows, err := queries.ListAgents(ctx, wsID)
	if err != nil {
		return "", fmt.Errorf("list_agents: %w", err)
	}

	type agentItem struct {
		ID     string `json:"id"`
		Name   string `json:"name"`
		Status string `json:"status"`
	}
	items := make([]agentItem, 0, len(rows))
	for _, r := range rows {
		items = append(items, agentItem{
			ID:     uuidStr(r.ID),
			Name:   r.Name,
			Status: r.Status,
		})
	}
	b, _ := json.Marshal(items)
	return string(b), nil
}

func toolAssignIssue(ctx context.Context, queries *db.Queries, wsID pgtype.UUID, args map[string]any) (string, error) {
	issueIDStr, _ := args["issue_id"].(string)
	assigneeType, _ := args["assignee_type"].(string)
	assigneeIDStr, _ := args["assignee_id"].(string)
	if issueIDStr == "" || assigneeType == "" || assigneeIDStr == "" {
		return "", fmt.Errorf("assign_issue: issue_id, assignee_type, and assignee_id are required")
	}
	if assigneeType != "member" && assigneeType != "agent" {
		return "", fmt.Errorf("assign_issue: assignee_type must be member or agent")
	}

	issueUUID, err := parseUUID(issueIDStr)
	if err != nil {
		return "", fmt.Errorf("assign_issue: invalid issue_id: %w", err)
	}
	assigneeUUID, err := parseUUID(assigneeIDStr)
	if err != nil {
		return "", fmt.Errorf("assign_issue: invalid assignee_id: %w", err)
	}

	if _, err := queries.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{
		ID:          issueUUID,
		WorkspaceID: wsID,
	}); err == pgx.ErrNoRows {
		return "", fmt.Errorf("issue not found")
	} else if err != nil {
		return "", fmt.Errorf("assign_issue: %w", err)
	}

	updated, err := queries.UpdateIssueAssignee(ctx, db.UpdateIssueAssigneeParams{
		ID:           issueUUID,
		AssigneeType: pgtype.Text{String: assigneeType, Valid: true},
		AssigneeID:   assigneeUUID,
	})
	if err != nil {
		return "", fmt.Errorf("assign_issue: %w", err)
	}

	assigneeName := assigneeIDStr
	if assigneeType == "member" {
		if m, err := queries.ListMembersWithUser(ctx, wsID); err == nil {
			for _, row := range m {
				if uuidStr(row.UserID) == uuidStr(updated.AssigneeID) {
					assigneeName = row.UserName
					break
				}
			}
		}
	} else {
		if agents, err := queries.ListAgents(ctx, wsID); err == nil {
			for _, a := range agents {
				if uuidStr(a.ID) == uuidStr(updated.AssigneeID) {
					assigneeName = a.Name
					break
				}
			}
		}
	}

	return fmt.Sprintf(`{"id":%q,"title":%q,"assignee_type":%q,"assignee_name":%q}`,
		uuidStr(updated.ID), updated.Title, assigneeType, assigneeName), nil
}

func toolTriggerAgent(ctx context.Context, queries *db.Queries, enqueuer TaskEnqueuer, wsID pgtype.UUID, args map[string]any) (string, error) {
	if enqueuer == nil {
		return "", fmt.Errorf("trigger_agent: agent dispatch is not available in this context")
	}

	issueIDStr, _ := args["issue_id"].(string)
	if issueIDStr == "" {
		return "", fmt.Errorf("trigger_agent: issue_id is required")
	}
	issueUUID, err := parseUUID(issueIDStr)
	if err != nil {
		return "", fmt.Errorf("trigger_agent: invalid issue_id: %w", err)
	}

	issue, err := queries.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{
		ID:          issueUUID,
		WorkspaceID: wsID,
	})
	if err == pgx.ErrNoRows {
		return "", fmt.Errorf("issue not found")
	}
	if err != nil {
		return "", fmt.Errorf("trigger_agent: %w", err)
	}
	if !issue.AssigneeID.Valid || issue.AssigneeType.String != "agent" {
		return "", fmt.Errorf("trigger_agent: issue %q does not have an agent assigned — use assign_issue with assignee_type=agent first", issueIDStr)
	}

	task, err := enqueuer.EnqueueTaskForIssue(ctx, issue)
	if err != nil {
		return "", fmt.Errorf("trigger_agent: %w", err)
	}

	return fmt.Sprintf(`{"task_id":%q,"issue_id":%q,"issue_title":%q,"status":"queued"}`,
		uuidStr(task.ID), uuidStr(issue.ID), issue.Title), nil
}

// helpers

func validStatus(s string) bool {
	switch s {
	case "todo", "in_progress", "in_review", "done":
		return true
	}
	return false
}

func validPriority(s string) bool {
	switch s {
	case "urgent", "high", "medium", "low", "no_priority":
		return true
	}
	return false
}

func uuidStr(u pgtype.UUID) string {
	if !u.Valid {
		return ""
	}
	b := u.Bytes
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func parseUUID(s string) (pgtype.UUID, error) {
	s = strings.ReplaceAll(s, "-", "")
	if len(s) != 32 {
		return pgtype.UUID{}, fmt.Errorf("invalid UUID length")
	}
	var b [16]byte
	for i := 0; i < 16; i++ {
		v, err := strconv.ParseUint(s[i*2:i*2+2], 16, 8)
		if err != nil {
			return pgtype.UUID{}, err
		}
		b[i] = byte(v)
	}
	return pgtype.UUID{Bytes: b, Valid: true}, nil
}
