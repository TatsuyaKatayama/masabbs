package integration

import (
	"testing"
	"time"

	"github.com/TatsuyaKatayama/masabbs/internal/auth"
	"github.com/nats-io/jwt/v2"
	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupAuthNATSServer(t *testing.T, authProvider *auth.Provider) (*server.Server, string) {
	opts := &server.Options{
		Host: "127.0.0.1",
		Port: -1,
	}

	opPubKey, _ := authProvider.OperatorKey.PublicKey()
	accPubKey, _ := authProvider.AccountKey.PublicKey()

	// Setup resolver (MemAccResolver)
	resolver := &server.MemAccResolver{}
	resolver.Store(accPubKey, authProvider.AccountJWT)

	opts.TrustedOperators = []*jwt.OperatorClaims{
		{
			ClaimsData: jwt.ClaimsData{
				Subject: opPubKey,
			},
		},
	}
	opts.AccountResolver = resolver

	ns, err := server.NewServer(opts)
	require.NoError(t, err)

	go ns.Start()
	if !ns.ReadyForConnections(5 * time.Second) {
		t.Fatal("NATS server failed to start")
	}

	return ns, "nats://" + ns.Addr().String()
}

func TestIntegration_AuthAndPermissions(t *testing.T) {
	provider, err := auth.NewProvider()
	require.NoError(t, err)

	ns, natsURL := setupAuthNATSServer(t, provider)
	defer ns.Shutdown()

	t.Run("UT-AUTH-103: Connect with unregistered agent (no JWT)", func(t *testing.T) {
		_, err := nats.Connect(natsURL, nats.Timeout(1*time.Second))
		assert.Error(t, err)
	})

	t.Run("UT-AUTH-105: Connect with expired JWT", func(t *testing.T) {
		creds, _ := provider.GenerateExpiredCredentials("agent-expired")

		authOpt := nats.UserJWTAndSeed(creds.JWT, creds.NKeySeed)
		_, err := nats.Connect(natsURL, authOpt, nats.Timeout(1*time.Second))
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "Authorization Violation")
	})

	t.Run("UT-AUTH-106: Connect with invalid signature", func(t *testing.T) {
		creds, _ := provider.GenerateInvalidSignatureCredentials("agent-invalid-sig")

		authOpt := nats.UserJWTAndSeed(creds.JWT, creds.NKeySeed)
		_, err := nats.Connect(natsURL, authOpt, nats.Timeout(1*time.Second))
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "Authorization Violation")
	})

	t.Run("Valid Manager Permissions (UT-AUTH-001)", func(t *testing.T) {
		creds, _ := provider.GenerateAgentCredentials("manager-1", "manager")
		authOpt := nats.UserJWTAndSeed(creds.JWT, creds.NKeySeed)

		nc, err := nats.Connect(natsURL, authOpt)
		require.NoError(t, err)
		defer nc.Close()

		err = nc.Publish("board.assign.123", []byte("test"))
		assert.NoError(t, err)

		var lastErr error
		nc.SetErrorHandler(func(conn *nats.Conn, subscription *nats.Subscription, e error) {
			lastErr = e
		})

		nc.Publish("board.event.manager-1", []byte("test"))
		nc.Flush()
		time.Sleep(100 * time.Millisecond)

		if lastErr != nil {
			assert.Contains(t, lastErr.Error(), "Permissions Violation")
		}
	})
}
