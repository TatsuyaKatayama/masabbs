package mentions

import (
	"context"
	"regexp"

	"github.com/jackc/pgx/v5/pgxpool"
)

var mentionRegex = regexp.MustCompile(`@([a-zA-Z0-9_-]+)`)

// ExtractMentions returns a list of unique agent IDs or "team" found in the text.
// The leading '@' is removed.
func ExtractMentions(text string) []string {
	matches := mentionRegex.FindAllStringSubmatch(text, -1)
	if len(matches) == 0 {
		return nil
	}

	unique := make(map[string]bool)
	var results []string
	for _, m := range matches {
		id := m[1]
		if !unique[id] {
			unique[id] = true
			results = append(results, id)
		}
	}
	return results
}

// Resolver handles mention expansion using the database
type Resolver struct {
	DB *pgxpool.Pool
}

// Result contains the resolved agent IDs and potential error codes
type Result struct {
	ToAgents  []string
	ErrorCode string
}

func NewResolver(db *pgxpool.Pool) *Resolver {
	return &Resolver{DB: db}
}

// Resolve parses a message and expands @agent-id and @team mentions.
func (r *Resolver) Resolve(ctx context.Context, message string, threadID string, fromAgent string) Result {
	matches := mentionRegex.FindAllStringSubmatch(message, -1)
	if len(matches) == 0 {
		return Result{ToAgents: []string{}}
	}

	uniqueMentions := make(map[string]bool)
	hasTeam := false
	for _, m := range matches {
		val := m[1]
		if val == "team" {
			hasTeam = true
		} else if val != fromAgent {
			uniqueMentions[val] = true
		}
	}

	resolvedIDs := make(map[string]bool)

	// 1. Resolve @team if present
	if hasTeam {
		// Get team_id for the thread
		var teamID *string
		err := r.DB.QueryRow(ctx, "SELECT team_id FROM threads WHERE id = $1", threadID).Scan(&teamID)
		if err != nil || teamID == nil {
			return Result{ErrorCode: "TEAM_CONTEXT_REQUIRED"}
		}

		// Get members of the team
		rows, err := r.DB.Query(ctx, "SELECT agent_id FROM team_agents WHERE team_id = $1", *teamID)
		if err != nil {
			return Result{ErrorCode: "TEAM_CONTEXT_REQUIRED"}
		}
		defer rows.Close()

		teamMemberFound := false
		for rows.Next() {
			var aid string
			if err := rows.Scan(&aid); err == nil {
				if aid != fromAgent {
					resolvedIDs[aid] = true
					teamMemberFound = true
				}
			}
		}
		if !teamMemberFound && len(uniqueMentions) == 0 {
			return Result{ErrorCode: "NO_TEAM_MEMBERS"}
		}
	}

	// 2. Resolve other @agent-id mentions
	for m := range uniqueMentions {
		var exists bool
		err := r.DB.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM agents WHERE id = $1)", m).Scan(&exists)
		if err != nil || !exists {
			return Result{ErrorCode: "UNKNOWN_MENTION"}
		}
		resolvedIDs[m] = true
	}

	final := make([]string, 0, len(resolvedIDs))
	for id := range resolvedIDs {
		final = append(final, id)
	}

	return Result{ToAgents: final}
}
