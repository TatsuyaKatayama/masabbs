package models

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCheckPermission(t *testing.T) {
	tests := []struct {
		name      string
		id        string
		role      string
		isPublish bool
		subject   string
		wantErr   error
	}{
		{
			name:      "UT-AUTH-001: manager can publish assign",
			id:        "UT-AUTH-001",
			role:      "manager",
			isPublish: true,
			subject:   "board.assign.123",
			wantErr:   nil,
		},
		{
			name:      "UT-AUTH-002: worker can publish result",
			id:        "UT-AUTH-002",
			role:      "worker",
			isPublish: true,
			subject:   "board.result.123",
			wantErr:   nil,
		},
		{
			name:      "UT-AUTH-003: observer can subscribe only",
			id:        "UT-AUTH-003",
			role:      "observer",
			isPublish: false,
			subject:   "board.task.123",
			wantErr:   nil,
		},
		{
			name:      "UT-AUTH-101: worker cannot publish assign",
			id:        "UT-AUTH-101",
			role:      "worker",
			isPublish: true,
			subject:   "board.assign.123",
			wantErr:   ErrUnauthorized,
		},
		{
			name:      "UT-AUTH-102: observer cannot publish",
			id:        "UT-AUTH-102",
			role:      "observer",
			isPublish: true,
			subject:   "board.task.123",
			wantErr:   ErrUnauthorized,
		},
		{
			name:      "UT-AUTH-109: NATS subject with wildcard publish",
			id:        "UT-AUTH-109",
			role:      "manager",
			isPublish: true,
			subject:   "board.*.>",
			wantErr:   ErrUnauthorized,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := CheckPermission(tt.role, tt.isPublish, tt.subject)
			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestThreadCreationPermissions(t *testing.T) {
	assert.True(t, CanCreateTopLevelThread(RoleTeamManager))
	assert.False(t, CanCreateTopLevelThread(RoleChef))
	assert.False(t, CanCreateTopLevelThread(RoleNewWorker))

	assert.True(t, CanCreateSubthread(RoleTeamManager))
	assert.False(t, CanCreateSubthread(RoleChef))
	assert.False(t, CanCreateSubthread(RoleNewWorker))
}
