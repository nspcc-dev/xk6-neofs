package native

import (
	"context"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nspcc-dev/neo-go/pkg/crypto/keys"
	cidtest "github.com/nspcc-dev/neofs-sdk-go/container/id/test"
	neofscrypto "github.com/nspcc-dev/neofs-sdk-go/crypto"
	neofsecdsa "github.com/nspcc-dev/neofs-sdk-go/crypto/ecdsa"
	oidtest "github.com/nspcc-dev/neofs-sdk-go/object/id/test"
	protonetmap "github.com/nspcc-dev/neofs-sdk-go/proto/netmap"
	protosession "github.com/nspcc-dev/neofs-sdk-go/proto/session"
	"github.com/nspcc-dev/neofs-sdk-go/version"
	"github.com/stretchr/testify/require"
	"go.k6.io/k6/js/modulestest"
	"go.k6.io/k6/lib"
	"go.k6.io/k6/metrics"
	"google.golang.org/grpc"
)

// A request that is not a stream gets the stream timeout as its limit, on a
// context of the VU's: it ends when the time is up, and when the VU does.
func TestUnaryContext(t *testing.T) {
	vu, stopVU := context.WithCancel(context.Background())
	defer stopVU()

	before := time.Now()
	ctx, cancel := unaryContext(vu, 50*time.Millisecond)
	after := time.Now()
	defer cancel()
	deadline, ok := ctx.Deadline()
	require.True(t, ok)
	require.False(t, deadline.Before(before.Add(50*time.Millisecond)))
	require.False(t, deadline.After(after.Add(50*time.Millisecond)))
	select {
	case <-ctx.Done():
		require.ErrorIs(t, ctx.Err(), context.DeadlineExceeded)
	case <-time.After(5 * time.Second):
		t.Fatal("a request with a limit of 50 ms was still waiting after 5 s")
	}

	// Without a stream timeout nothing changes: the VU's own context.
	ctx, cancel = unaryContext(vu, 0)
	defer cancel()
	_, ok = ctx.Deadline()
	require.False(t, ok)
	require.Equal(t, vu, ctx)

	// The VU ends before the time is up.
	ctx, cancel = unaryContext(vu, time.Hour)
	defer cancel()
	stopVU()
	<-ctx.Done()
	require.ErrorIs(t, ctx.Err(), context.Canceled)
}

// quietNode is a storage node that answers what a client asks when it
// connects, and then nothing: every object request waits until the client
// gives up. With deaf set it does not open a session either.
type quietNode struct {
	protonetmap.UnimplementedNetmapServiceServer
	protosession.UnimplementedSessionServiceServer

	key  *keys.PrivateKey
	deaf atomic.Bool
	// waiting counts the requests that are waiting for an answer.
	waiting atomic.Int64
}

func (n *quietNode) signer() neofscrypto.Signer { return neofsecdsa.Signer(n.key.PrivateKey) }

func (n *quietNode) LocalNodeInfo(context.Context, *protonetmap.LocalNodeInfoRequest) (*protonetmap.LocalNodeInfoResponse, error) {
	resp := &protonetmap.LocalNodeInfoResponse{Body: &protonetmap.LocalNodeInfoResponse_Body{
		Version:  version.Current().ProtoMessage(),
		NodeInfo: &protonetmap.NodeInfo{PublicKey: n.key.PublicKey().Bytes(), Addresses: []string{"/ip4/127.0.0.1/tcp/8080"}},
	}}
	var err error
	resp.VerifyHeader, err = neofscrypto.SignResponseWithBuffer(n.signer(), resp, nil)
	return resp, err
}

func (n *quietNode) Create(ctx context.Context, _ *protosession.CreateRequest) (*protosession.CreateResponse, error) {
	if n.deaf.Load() {
		return nil, n.wait(ctx)
	}
	id := uuid.New()
	resp := &protosession.CreateResponse{Body: &protosession.CreateResponse_Body{Id: id[:], SessionKey: n.key.PublicKey().Bytes()}}
	var err error
	resp.VerifyHeader, err = neofscrypto.SignResponseWithBuffer(n.signer(), resp, nil)
	return resp, err
}

func (n *quietNode) wait(ctx context.Context) error {
	n.waiting.Add(1)
	defer n.waiting.Add(-1)
	<-ctx.Done()
	return ctx.Err()
}

// startQuietNode serves a quietNode on a local port and returns it with its
// address.
func startQuietNode(t *testing.T) (*quietNode, string) {
	t.Helper()
	key, err := keys.NewPrivateKey()
	require.NoError(t, err)
	n := &quietNode{key: key}
	// Anything but the two services below is asked of nobody.
	srv := grpc.NewServer(grpc.UnknownServiceHandler(func(_ any, stream grpc.ServerStream) error {
		return n.wait(stream.Context())
	}))
	protonetmap.RegisterNetmapServiceServer(srv, n)
	protosession.RegisterSessionServiceServer(srv, n)
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)
	return n, lis.Addr().String()
}

// testVU returns the module as a VU has it during a test run: made in the
// init context, where its metrics are registered, and then given a state to
// send samples to. The samples arrive in the channel it returns.
func testVU(t *testing.T) (*modulestest.Runtime, *Native, chan metrics.SampleContainer) {
	t.Helper()
	rt := modulestest.NewRuntime(t)
	n := new(RootModule).NewModuleInstance(rt.VU).(*Native)
	registry := metrics.NewRegistry()
	samples := make(chan metrics.SampleContainer, 100)
	rt.MoveToVUContext(&lib.State{
		Samples:        samples,
		Tags:           lib.NewVUStateTags(registry.RootTagSet()),
		BuiltinMetrics: metrics.RegisterBuiltinMetrics(registry),
	})
	return rt, n, samples
}

// HEAD, DELETE and LIST are single requests, not streams, and the SDK gives
// them no deadline. To a node that stops answering they must not wait for as
// long as the VU lives: with a stream timeout they end when it is up, as
// failed requests.
func TestRequestsToAQuietNodeEnd(t *testing.T) {
	node, addr := startQuietNode(t)
	_, n, samples := testVU(t)

	c, err := n.Connect("grpc://"+addr, "", 5, 1)
	require.NoError(t, err)
	cnr, obj := cidtest.ID().String(), oidtest.ID().String()

	for name, request := range map[string]func() (ok bool, msg string){
		"HEAD":   func() (bool, string) { r := c.Head(cnr, obj); return r.Success, r.Error },
		"DELETE": func() (bool, string) { r := c.Delete(cnr, obj); return r.Success, r.Error },
		"LIST":   func() (bool, string) { r := c.List(cnr, "prefix/"); return r.Success, r.Error },
	} {
		start := time.Now()
		ok, msg := request()
		took := time.Since(start)
		require.False(t, ok, name)
		require.Contains(t, msg, "context deadline exceeded", name)
		require.GreaterOrEqual(t, took, 900*time.Millisecond, name)
		require.Less(t, took, 4*time.Second, "%s was not ended by the stream timeout of 1 s", name)
		// And it is reported, as a request that was made and failed: the
		// VU is still there to report it.
		require.Len(t, samples, 1, name)
		reported := (<-samples).GetSamples()
		require.Len(t, reported, 2, name)
		require.Contains(t, reported[0].Metric.Name, "_total", name)
		require.Contains(t, reported[1].Metric.Name, "_fails", name)
	}

	// Without a stream timeout nothing changes: such a request waits for
	// as long as the VU lives.
	rt, n, _ := testVU(t)
	vu, stopVU := context.WithCancel(rt.VU.Context())
	rt.VU.CtxField = vu
	c, err = n.Connect("grpc://"+addr, "", 5, 0)
	require.NoError(t, err)
	done := make(chan HeadResponse, 1)
	go func() { done <- c.Head(cnr, obj) }()
	require.Eventually(t, func() bool { return node.waiting.Load() == 1 }, 5*time.Second, 10*time.Millisecond)
	select {
	case r := <-done:
		t.Fatalf("a request without a limit ended by itself: %+v", r)
	case <-time.After(1500 * time.Millisecond):
	}
	stopVU()
	select {
	case r := <-done:
		require.False(t, r.Success)
	case <-time.After(5 * time.Second):
		t.Fatal("the request did not end with the VU")
	}

	// connect() itself asks the node to open a session. That is part of
	// connecting and has the dial timeout, whatever the stream timeout: it
	// is made in the init stage, on a context that nothing would end.
	node.deaf.Store(true)
	_, n, _ = testVU(t)
	start := time.Now()
	_, err = n.Connect("grpc://"+addr, "", 1, 0)
	require.ErrorContains(t, err, "context deadline exceeded")
	require.GreaterOrEqual(t, time.Since(start), 900*time.Millisecond)
	require.Less(t, time.Since(start), 4*time.Second)
}
