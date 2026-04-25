package auth

import (
	"fmt"
	"time"

	"github.com/nats-io/jwt/v2"
	"github.com/nats-io/nkeys"
)

type Provider struct {
	OperatorKey nkeys.KeyPair
	AccountKey  nkeys.KeyPair
	AccountJWT  string
}

// NewProvider initializes the NATS Operator and Account for the system.
// In a real production system, these keys should be loaded from secure storage.
func NewProvider() (*Provider, error) {
	// Create Operator
	opKey, err := nkeys.CreateOperator()
	if err != nil {
		return nil, fmt.Errorf("failed to create operator key: %w", err)
	}
	opPubKey, _ := opKey.PublicKey()

	opClaims := jwt.NewOperatorClaims(opPubKey)
	opClaims.Name = "masabbs-operator"

	// Create Account
	accKey, err := nkeys.CreateAccount()
	if err != nil {
		return nil, fmt.Errorf("failed to create account key: %w", err)
	}
	accPubKey, _ := accKey.PublicKey()

	accClaims := jwt.NewAccountClaims(accPubKey)
	accClaims.Name = "masabbs-account"
	
	// Sign Account with Operator
	accJWT, err := accClaims.Encode(opKey)
	if err != nil {
		return nil, fmt.Errorf("failed to sign account jwt: %w", err)
	}

	return &Provider{
		OperatorKey: opKey,
		AccountKey:  accKey,
		AccountJWT:  accJWT,
	}, nil
}

type Credentials struct {
	AgentID  string `json:"agent_id"`
	NKeySeed string `json:"nkey_seed"`
	JWT      string `json:"jwt"`
}

// GenerateAgentCredentials generates a NKey pair and a JWT for an agent based on its role.
func (p *Provider) GenerateAgentCredentials(agentID, role string) (*Credentials, error) {
	userKey, err := nkeys.CreateUser()
	if err != nil {
		return nil, fmt.Errorf("failed to create user key: %w", err)
	}
	userPubKey, _ := userKey.PublicKey()

	userClaims := jwt.NewUserClaims(userPubKey)
	userClaims.Name = agentID

	// Define permissions based on role
	switch role {
	case "manager":
		userClaims.Pub.Allow.Add("board.tasks", "board.task.*", "board.assign.*")
		userClaims.Sub.Allow.Add("board.offer.*", "board.result.*", "board.status.*", "board.event.*")
	case "worker":
		userClaims.Pub.Allow.Add("board.offer.*", "board.result.*", "board.status.*")
		userClaims.Sub.Allow.Add("board.tasks", "board.task.*", "board.assign.*", "board.event.*")
	case "observer":
		// Publish is empty
		userClaims.Sub.Allow.Add("board.task.*", "board.result.*", "board.status.*", "board.event.*")
	case "admin": // For management server / operations
		userClaims.Pub.Allow.Add("board.>")
		userClaims.Sub.Allow.Add("board.>")
	default:
		return nil, fmt.Errorf("unknown role: %s", role)
	}

	// Sign User JWT with Account Key
	userJWT, err := userClaims.Encode(p.AccountKey)
	if err != nil {
		return nil, fmt.Errorf("failed to sign user jwt: %w", err)
	}

	seed, err := userKey.Seed()
	if err != nil {
		return nil, fmt.Errorf("failed to extract user seed: %w", err)
	}

	return &Credentials{
		AgentID:  agentID,
		NKeySeed: string(seed),
		JWT:      userJWT,
	}, nil
}

// GenerateExpiredCredentials generates a JWT that is already expired for testing (UT-AUTH-105).
func (p *Provider) GenerateExpiredCredentials(agentID string) (*Credentials, error) {
	userKey, _ := nkeys.CreateUser()
	userPubKey, _ := userKey.PublicKey()

	userClaims := jwt.NewUserClaims(userPubKey)
	userClaims.Name = agentID
	userClaims.Expires = time.Now().Add(-1 * time.Hour).Unix() // Expired 1 hour ago

	userJWT, _ := userClaims.Encode(p.AccountKey)
	seed, _ := userKey.Seed()

	return &Credentials{
		AgentID:  agentID,
		NKeySeed: string(seed),
		JWT:      userJWT,
	}, nil
}

// GenerateInvalidSignatureCredentials generates a JWT signed by a different (invalid) account key (UT-AUTH-106).
func (p *Provider) GenerateInvalidSignatureCredentials(agentID string) (*Credentials, error) {
	userKey, _ := nkeys.CreateUser()
	userPubKey, _ := userKey.PublicKey()

	userClaims := jwt.NewUserClaims(userPubKey)
	userClaims.Name = agentID

	// Sign with a completely different, unauthorized account key
	invalidAccKey, _ := nkeys.CreateAccount()
	userJWT, _ := userClaims.Encode(invalidAccKey)
	seed, _ := userKey.Seed()

	return &Credentials{
		AgentID:  agentID,
		NKeySeed: string(seed),
		JWT:      userJWT,
	}, nil
}

func (p *Provider) RevokeAgent(agentID string) error {
	return nil
}
