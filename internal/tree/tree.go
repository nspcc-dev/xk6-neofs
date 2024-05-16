package tree

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/nspcc-dev/neo-go/pkg/crypto/keys"
	"github.com/nspcc-dev/neofs-node/pkg/services/tree"
	cid "github.com/nspcc-dev/neofs-sdk-go/container/id"
	"github.com/nspcc-dev/xk6-neofs/internal/stats"
	"go.k6.io/k6/js/common"
	"go.k6.io/k6/js/modules"
	"go.k6.io/k6/metrics"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func init() {
	modules.Register("k6/x/neofs/tree", new(Root))
}

type Root struct{}

func (t Root) NewModuleInstance(vu modules.VU) modules.Instance {
	m, err := registerMetrics(vu)
	if err != nil {
		common.Throw(vu.Runtime(), err)
	}

	return &Tree{vu: vu, metrics: m}
}

type Tree struct {
	vu      modules.VU
	metrics treeMetrics
}

func (t *Tree) Exports() modules.Exports {
	return modules.Exports{
		Default: t,
	}
}

var (
	_ modules.Instance = &Tree{}
	_ modules.Module   = &Root{}
)

func (t *Tree) Client(endpoint string) *Client {
	const defaultClientConnectTimeout = time.Second * 2

	ctx, cancel := context.WithTimeout(t.vu.Context(), defaultClientConnectTimeout)
	conn, err := grpc.DialContext(ctx, endpoint, grpc.WithBlock(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	cancel()

	if err != nil {
		common.Throw(t.vu.Runtime(), fmt.Errorf("client dial: %w", err))
	}

	pk, err := keys.NewPrivateKey()
	if err != nil {
		common.Throw(t.vu.Runtime(), fmt.Errorf("private key generation: %w", err))
	}

	return &Client{
		vu:      t.vu,
		metrics: t.metrics,
		c:       tree.NewTreeServiceClient(conn),
		pk:      &pk.PrivateKey,
	}
}

type Client struct {
	vu      modules.VU
	metrics treeMetrics

	c   tree.TreeServiceClient
	cID cid.ID
	pk  *ecdsa.PrivateKey

	reqNumber atomic.Int32
}

type ClientResponse struct {
	Success bool
	Error   string
}

func (c *Client) Add(tID string) ClientResponse {
	rawCID := make([]byte, 32)
	c.cID.Encode(rawCID)

	uniqueID := c.reqNumber.Add(1)
	metaKey := strconv.Itoa(int(uniqueID))
	metaVal := []byte(metaKey)
	metaPair := &tree.KeyValue{Key: metaKey, Value: metaVal}

	req := new(tree.AddRequest)
	req.Body = &tree.AddRequest_Body{
		ContainerId: rawCID,
		TreeId:      tID,
		ParentId:    0,
		Meta: []*tree.KeyValue{ // S3 GW has smth about 7 meta fields usually
			metaPair,
			metaPair,
			metaPair,
			metaPair,
			metaPair,
			metaPair,
			metaPair,
		},
	}

	err := tree.SignMessage(req, c.pk)
	if err != nil {
		return ClientResponse{
			Error: fmt.Sprintf("sign ADD request: %s", err),
		}
	}

	stats.Report(c.vu, c.metrics.AddTotal, 1)
	startedAt := time.Now()

	ctx, cancel := context.WithTimeout(c.vu.Context(), 5*time.Second)
	defer cancel()

	_, err = c.c.Add(ctx, req)
	if err != nil {
		stats.Report(c.vu, c.metrics.ErrRate, 1)
		stats.Report(c.vu, c.metrics.AddFails, 1)
		return ClientResponse{
			Error: fmt.Sprintf("network request: %s", err),
		}
	}

	stats.Report(c.vu, c.metrics.AddDuration, metrics.D(time.Since(startedAt)))
	stats.Report(c.vu, c.metrics.ErrRate, 0)
	stats.ReportDataSent(c.vu, float64(req.StableSize()))

	return ClientResponse{Success: true}
}

func (c *Client) AddByPath(tID string) ClientResponse {
	rawCID := make([]byte, 32)
	c.cID.Encode(rawCID)

	uniqueID := c.reqNumber.Add(1)
	metaKey := strconv.Itoa(int(uniqueID))
	metaVal := []byte(metaKey)
	metaPair := &tree.KeyValue{Key: metaKey, Value: metaVal}

	req := new(tree.AddByPathRequest)
	req.Body = &tree.AddByPathRequest_Body{
		ContainerId: rawCID,
		TreeId:      tID,
		Path:        []string{"test", "only", "path", metaKey},
		Meta: []*tree.KeyValue{ // S3 GW has smth about 7 meta fields usually
			metaPair,
			metaPair,
			metaPair,
			metaPair,
			metaPair,
			metaPair,
			metaPair},
	}

	err := tree.SignMessage(req, c.pk)
	if err != nil {
		return ClientResponse{
			Error: fmt.Sprintf("sign ADD_BY_PATH request: %s", err),
		}
	}

	ctx, cancel := context.WithTimeout(c.vu.Context(), 5*time.Second)
	defer cancel()

	stats.Report(c.vu, c.metrics.AddByPathTotal, 1)
	startedAt := time.Now()

	_, err = c.c.AddByPath(ctx, req)
	if err != nil {
		stats.Report(c.vu, c.metrics.ErrRate, 0)
		stats.Report(c.vu, c.metrics.AddByPathFails, 1)
		return ClientResponse{
			Error: fmt.Sprintf("network request: %s", err),
		}
	}

	stats.Report(c.vu, c.metrics.AddByPathDuration, metrics.D(time.Since(startedAt)))
	stats.Report(c.vu, c.metrics.ErrRate, 1)
	stats.ReportDataSent(c.vu, float64(req.StableSize()))

	return ClientResponse{Success: true}
}
