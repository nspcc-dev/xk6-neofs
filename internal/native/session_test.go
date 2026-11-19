package native

import (
	"errors"
	"testing"
	"time"

	"github.com/nspcc-dev/neo-go/pkg/crypto/keys"
	cidtest "github.com/nspcc-dev/neofs-sdk-go/container/id/test"
	neofsecdsa "github.com/nspcc-dev/neofs-sdk-go/crypto/ecdsa"
	"github.com/nspcc-dev/neofs-sdk-go/session/v2"
	"github.com/nspcc-dev/neofs-sdk-go/user"
	"github.com/stretchr/testify/require"
)

// noNNS fails the test's token validation if it ever needs a name resolved:
// the token names its subject by ID.
type noNNS struct{}

func (noNNS) HasUser(string, user.ID) (bool, error) {
	return false, errors.New("unexpected NNS request")
}

func TestNewSessionToken(t *testing.T) {
	pk, err := keys.NewPrivateKey()
	require.NoError(t, err)
	sessionPK, err := keys.NewPrivateKey()
	require.NoError(t, err)
	signer := user.NewAutoIDSignerRFC6979(pk.PrivateKey)
	sessionKey := neofsecdsa.PublicKey(sessionPK.PrivateKey.PublicKey)
	now := time.Now()

	tok, err := newSessionToken(signer, sessionKey, now)
	require.NoError(t, err)

	// What a node checks before it accepts the token.
	require.NoError(t, tok.Validate(noNNS{}))
	require.True(t, tok.VerifySignature())
	require.Equal(t, signer.UserID(), tok.Issuer())
	require.True(t, tok.ValidAt(now))
	require.True(t, tok.ValidAt(now.Add(365*24*time.Hour)))

	// One token for every operation of the client in any container, and for
	// nothing else.
	for range 2 {
		cnr := cidtest.ID()
		for _, verb := range sessionVerbs {
			require.True(t, tok.AssertVerb(verb, cnr), verb)
		}
		require.False(t, tok.AssertVerb(session.VerbObjectSearch, cnr))
		require.False(t, tok.AssertVerb(session.VerbContainerDelete, cnr))
	}

	// The node may act within the session with the key it created for it.
	require.Equal(t, []session.Target{session.NewTargetUser(user.NewFromECDSAPublicKey(sessionPK.PrivateKey.PublicKey))},
		tok.Subjects())
	require.True(t, tok.IsFinal())
}
