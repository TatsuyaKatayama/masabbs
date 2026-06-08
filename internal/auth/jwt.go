package auth

import (
	"encoding/base64"
	"fmt"
	"sync"
	"time"

	"github.com/nats-io/jwt/v2"
	"github.com/nats-io/nkeys"
)

type Provider struct {
	OperatorKey nkeys.KeyPair
	AccountKey  nkeys.KeyPair
	AccountJWT  string
	revocations map[string]time.Time // AgentID -> Expiry
	pubKeys     map[string]string    // AgentID -> UserPubKey
	mu          sync.RWMutex
}

// NewProvider initializes the NATS Operator and Account for the system.
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
		revocations: make(map[string]time.Time),
		pubKeys:     make(map[string]string),
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

	p.mu.Lock()
	p.pubKeys[agentID] = userPubKey
	p.mu.Unlock()

	userClaims := jwt.NewUserClaims(userPubKey)
	userClaims.Name = agentID

	// Define permissions based on role
	switch role {
	case "manager":
		userClaims.Pub.Allow.Add("board.tasks", "board.task.*", "board.assign.*")
		userClaims.Sub.Allow.Add("board.offer.*", "board.result.*", "board.event.*")
	case "worker":
		userClaims.Pub.Allow.Add("board.offer.*", "board.result.*")
		userClaims.Sub.Allow.Add("board.tasks", "board.task.*", "board.assign.*", "board.event.*")
	case "observer":
		// Publish is empty
		userClaims.Sub.Allow.Add("board.task.*", "board.result.*", "board.event.*")
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

// GenerateExpiredCredentials generates a JWT that is already expired for testing.
func (p *Provider) GenerateExpiredCredentials(agentID string) (*Credentials, error) {
	userKey, _ := nkeys.CreateUser()
	userPubKey, _ := userKey.PublicKey()

	userClaims := jwt.NewUserClaims(userPubKey)
	userClaims.Name = agentID
	userClaims.Expires = time.Now().Add(-1 * time.Hour).Unix()

	userJWT, _ := userClaims.Encode(p.AccountKey)
	seed, _ := userKey.Seed()

	return &Credentials{
		AgentID:  agentID,
		NKeySeed: string(seed),
		JWT:      userJWT,
	}, nil
}

// GenerateInvalidSignatureCredentials generates a JWT signed by a different (invalid) account key.
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

// RevokeAgent temporarily blocks an agent.
func (p *Provider) RevokeAgent(agentID string, duration time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.revocations[agentID] = time.Now().Add(duration)
}

// IsRevoked checks if an agent is currently blocked.
func (p *Provider) IsRevoked(agentID string) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	expiry, ok := p.revocations[agentID]
	if !ok {
		return false
	}
	if time.Now().After(expiry) {
		return false
	}
	return true
}

// VerifySignature validates that the message was indeed signed by the claimed agent.
func (p *Provider) VerifySignature(agentID string, data []byte, signature string) error {
	p.mu.RLock()
	userPubKey, ok := p.pubKeys[agentID]
	p.mu.RUnlock()

	if !ok {
		return fmt.Errorf("public key not found for agent: %s", agentID)
	}

	pub, err := nkeys.FromPublicKey(userPubKey)
	if err != nil {
		return fmt.Errorf("invalid public key: %w", err)
	}

	sig, err := base64.StdEncoding.DecodeString(signature)
	if err != nil {
		return fmt.Errorf("invalid signature encoding: %w", err)
	}

	return pub.Verify(data, sig)
}

// SignMessage signs a byte array with an agent's seed (for testing/simulation).
func (p *Provider) SignMessage(seed string, data []byte) (string, error) {
	kp, err := nkeys.FromSeed([]byte(seed))
	if err != nil {
		return "", err
	}
	sig, err := kp.Sign(data)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(sig), nil
}

// GetRevocationList returns a map of Public Key to Revocation Time for NATS Account Claims.
func (p *Provider) GetRevocationList() map[string]int64 {
	p.mu.RLock()
	defer p.mu.RUnlock()

	revs := make(map[string]int64)
	now := time.Now()
	for id, expiry := range p.revocations {
		if expiry.After(now) {
			if pubKey, ok := p.pubKeys[id]; ok {
				revs[pubKey] = now.Unix()
			}
		}
	}
	return revs
}
