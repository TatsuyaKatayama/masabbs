package models

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestStateTransitions(t *testing.T) {
	tests := []struct {
		name         string
		id           string
		initialState string
		transitions  []string
		wantErr      error
		expected     string
	}{
		{
			name:         "UT-SM-001: open -> assigned -> processing -> done",
			id:           "UT-SM-001",
			initialState: StateOpen,
			transitions:  []string{StateAssigned, StateProcessing, StateDone},
			wantErr:      nil,
			expected:     StateDone,
		},
		{
			name:         "UT-SM-002: open -> collecting -> done",
			id:           "UT-SM-002",
			initialState: StateOpen,
			transitions:  []string{StateCollecting, StateDone},
			wantErr:      nil,
			expected:     StateDone,
		},
		{
			name:         "UT-SM-101: done -> processing (reverse)",
			id:           "UT-SM-101",
			initialState: StateDone,
			transitions:  []string{StateProcessing},
			wantErr:      ErrInvalidStateTransition,
			expected:     StateDone, // Should not change
		},
		{
			name:         "UT-SM-102: error -> processing",
			id:           "UT-SM-102",
			initialState: StateError,
			transitions:  []string{StateProcessing},
			wantErr:      ErrInvalidStateTransition,
			expected:     StateError,
		},
		{
			name:         "UT-SM-103: assigned -> open (rewind)",
			id:           "UT-SM-103",
			initialState: StateAssigned,
			transitions:  []string{StateOpen},
			wantErr:      ErrInvalidStateTransition,
			expected:     StateAssigned,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			thread := &Thread{Status: tt.initialState}
			var lastErr error
			for _, nextState := range tt.transitions {
				err := thread.Transition(nextState)
				if err != nil {
					lastErr = err
				}
			}

			if tt.wantErr != nil {
				assert.ErrorIs(t, lastErr, tt.wantErr)
			} else {
				assert.NoError(t, lastErr)
			}
			assert.Equal(t, tt.expected, thread.Status)
		})
	}
}
