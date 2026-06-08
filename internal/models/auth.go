package models

import (
	"errors"
	"strings"
)

var (
	ErrUnauthorized = errors.New("unauthorized action for role")
)

// CheckPermission checks if an agent with a specific role can publish/subscribe to a subject
func CheckPermission(role string, isPublish bool, subject string) error {
	// Simple rule-based permission check based on specification
	// In reality, this matches NATS JWT settings.
	// Subject formats: board.task.*, board.assign.*, etc.

	parts := strings.Split(subject, ".")
	if len(parts) < 2 || parts[0] != "board" {
		// Wildcard board.*.> is forbidden for agents
		if strings.Contains(subject, "*") || strings.Contains(subject, ">") {
			return ErrUnauthorized
		}
	}

	msgType := ""
	if len(parts) >= 2 {
		msgType = parts[1]
	}

	// Reject wildcard publishing for safety (UT-AUTH-109)
	if isPublish && (strings.Contains(subject, "*") || strings.Contains(subject, ">")) {
		return ErrUnauthorized
	}

	switch role {
	case "manager":
		if isPublish {
			if msgType == "tasks" || msgType == "task" || msgType == "assign" {
				return nil
			}
		} else {
			if msgType == "offer" || msgType == "result" {
				return nil
			}
		}
	case "worker":
		if isPublish {
			if msgType == "offer" || msgType == "result" {
				return nil
			}
		} else {
			if msgType == "tasks" || msgType == "task" || msgType == "assign" {
				return nil
			}
		}
	case "observer":
		if isPublish {
			return ErrUnauthorized // Observer cannot publish
		} else {
			if msgType == "tasks" || msgType == "task" || msgType == "result" {
				return nil
			}
		}
	}

	return ErrUnauthorized
}
