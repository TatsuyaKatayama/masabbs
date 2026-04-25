package models

import (
	"errors"
)

var (
	ErrInvalidStateTransition = errors.New("invalid state transition")
)

const (
	StateOpen       = "open"
	StateAssigned   = "assigned"
	StateCollecting = "collecting"
	StateProcessing = "processing"
	StateDone       = "done"
	StateError      = "error"
)

// AllowedTransitions maps a state to the slice of states it can transition to.
var AllowedTransitions = map[string][]string{
	StateOpen:       {StateAssigned, StateCollecting, StateError},
	StateAssigned:   {StateProcessing, StateError},
	StateCollecting: {StateDone, StateError},
	StateProcessing: {StateDone, StateError},
	StateDone:       {}, // Terminal state
	StateError:      {}, // Terminal state, or perhaps can be retried but spec says error -> processing is denied
}

// Transition changes the thread status if the transition is valid
func (t *Thread) Transition(newState string) error {
	allowed, ok := AllowedTransitions[t.Status]
	if !ok {
		return ErrInvalidStateTransition
	}

	isValid := false
	for _, s := range allowed {
		if s == newState {
			isValid = true
			break
		}
	}

	if !isValid {
		return ErrInvalidStateTransition
	}

	t.Status = newState
	return nil
}
