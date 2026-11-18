package s3

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/stretchr/testify/require"
	"go.k6.io/k6/js/modulestest"
	"go.k6.io/k6/lib"
	"go.k6.io/k6/metrics"
)

type closingTracker struct {
	base   http.RoundTripper
	closed atomic.Int32
}

func (t *closingTracker) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.base.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	resp.Body = &trackedBody{ReadCloser: resp.Body, closed: &t.closed}
	return resp, nil
}

type trackedBody struct {
	io.ReadCloser
	closed *atomic.Int32
}

func (b *trackedBody) Close() error {
	b.closed.Add(1)
	return b.ReadCloser.Close()
}

func newTestClient(srv *httptest.Server) (*s3.Client, *closingTracker) {
	tracker := &closingTracker{base: srv.Client().Transport}
	cli := s3.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("a", "b", ""),
	}, func(options *s3.Options) {
		options.BaseEndpoint = aws.String(srv.URL)
		options.UsePathStyle = true
		options.Retryer = aws.NopRetryer{}
		options.HTTPClient = &http.Client{Transport: tracker}
	})
	return cli, tracker
}

// newVUTestClient returns a client of the endpoint whose VU sends its
// samples to the returned channel.
func newVUTestClient(t *testing.T, srv *httptest.Server) (*Client, chan metrics.SampleContainer) {
	t.Helper()

	rt := modulestest.NewRuntime(t)
	samples := make(chan metrics.SampleContainer, 16)
	// Module instances are created in the init context, before the VU runs.
	instance := new(RootModule).NewModuleInstance(rt.VU).(*S3)
	rt.MoveToVUContext(&lib.State{
		Samples: samples,
		Tags:    lib.NewVUStateTags(rt.VU.InitEnvField.Registry.RootTagSet()),
	})

	cli, _ := newTestClient(srv)
	return &Client{vu: rt.VU, metrics: instance.metrics, cli: cli}, samples
}

// requireSamples checks that the channel holds samples of exactly the
// given metrics.
func requireSamples(t *testing.T, samples chan metrics.SampleContainer, want ...string) {
	t.Helper()

	var got []string
	for len(samples) > 0 {
		for _, sample := range (<-samples).GetSamples() {
			got = append(got, sample.Metric.Name)
		}
	}
	require.ElementsMatch(t, want, got)
}

type listRequest struct {
	continuationToken string
	prefix            string
}

const listFirstPageXML = `<?xml version="1.0" encoding="UTF-8"?>
<ListBucketResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">
	<Name>bucket</Name>
	<Prefix>dir/</Prefix>
	<KeyCount>2</KeyCount>
	<MaxKeys>1000</MaxKeys>
	<IsTruncated>true</IsTruncated>
	<Contents><Key>dir/a</Key></Contents>
	<Contents><Key>dir/b</Key></Contents>
	<NextContinuationToken>t1</NextContinuationToken>
</ListBucketResult>`

const listSecondPageXML = `<?xml version="1.0" encoding="UTF-8"?>
<ListBucketResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">
	<Name>bucket</Name>
	<Prefix>dir/</Prefix>
	<KeyCount>1</KeyCount>
	<MaxKeys>1000</MaxKeys>
	<IsTruncated>false</IsTruncated>
	<Contents><Key>dir/c</Key></Contents>
</ListBucketResult>`

func TestListFollowsAllPages(t *testing.T) {
	var (
		mu       sync.Mutex
		requests []listRequest
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		mu.Lock()
		requests = append(requests, listRequest{
			continuationToken: query.Get("continuation-token"),
			prefix:            query.Get("prefix"),
		})
		mu.Unlock()

		w.Header().Set("Content-Type", "application/xml")
		if query.Get("continuation-token") == "" {
			_, _ = io.WriteString(w, listFirstPageXML)
			return
		}
		_, _ = io.WriteString(w, listSecondPageXML)
	}))
	defer srv.Close()

	client, samples := newVUTestClient(t, srv)
	resp := client.List("bucket", "dir/", nil)

	require.True(t, resp.Success)
	require.Empty(t, resp.Error)
	require.Equal(t, 3, resp.Count)
	requireSamples(t, samples, "aws_obj_list_total", "aws_obj_list_duration")

	mu.Lock()
	gotRequests := append([]listRequest(nil), requests...)
	mu.Unlock()
	require.Equal(t, []listRequest{
		{prefix: "dir/"},
		{continuationToken: "t1", prefix: "dir/"},
	}, gotRequests)
}

func TestListFailsWhenPageFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("continuation-token") == "" {
			w.Header().Set("Content-Type", "application/xml")
			_, _ = io.WriteString(w, listFirstPageXML)
			return
		}
		http.Error(w, "listing failed", http.StatusInternalServerError)
	}))
	defer srv.Close()

	client, samples := newVUTestClient(t, srv)
	resp := client.List("bucket", "", nil)

	require.False(t, resp.Success)
	require.NotEmpty(t, resp.Error)
	requireSamples(t, samples, "aws_obj_list_total", "aws_obj_list_fails")
}

func TestListFetchesOwnerOnRequest(t *testing.T) {
	var fetchOwner []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fetchOwner = append(fetchOwner, r.URL.Query().Get("fetch-owner"))
		w.Header().Set("Content-Type", "application/xml")
		_, _ = io.WriteString(w, listSecondPageXML)
	}))
	defer srv.Close()

	client, _ := newVUTestClient(t, srv)
	require.True(t, client.List("bucket", "", nil).Success)
	require.True(t, client.List("bucket", "", map[string]string{"fetch_owner": "true"}).Success)
	require.Equal(t, []string{"", "true"}, fetchOwner)

	resp := client.List("bucket", "", map[string]string{"fetch_owner": "maybe"})
	require.False(t, resp.Success)
	require.Contains(t, resp.Error, "fetch_owner")
}

func TestGetReturnsErrorOnTruncatedBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "100")
		_, _ = w.Write([]byte("0123456789"))
	}))
	defer srv.Close()

	cli, tracker := newTestClient(srv)
	got, err := get(context.Background(), cli, "bucket", "key", io.Discard)

	require.Error(t, err)
	require.ErrorContains(t, err, "read object body")
	require.Equal(t, int64(10), got)
	require.Equal(t, int32(1), tracker.closed.Load())
}

func TestGetReadsWholeBody(t *testing.T) {
	payload := make([]byte, 100*1024)
	for i := range payload {
		payload[i] = byte(i)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
		_, _ = w.Write(payload)
	}))
	defer srv.Close()

	cli, tracker := newTestClient(srv)
	var got bytes.Buffer
	n, err := get(context.Background(), cli, "bucket", "key", &got)

	require.NoError(t, err)
	require.Equal(t, int64(len(payload)), n)
	require.Equal(t, payload, got.Bytes())
	require.Equal(t, int32(1), tracker.closed.Load())
}

func TestHeadReportsSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c, samples := newVUTestClient(t, srv)

	resp := c.Head("bucket", "key")
	require.True(t, resp.Success)
	require.Empty(t, resp.Error)

	requireSamples(t, samples, "aws_obj_head_total", "aws_obj_head_duration")
}

func TestHeadReportsFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c, samples := newVUTestClient(t, srv)

	resp := c.Head("bucket", "key")
	require.False(t, resp.Success)
	require.NotEmpty(t, resp.Error)

	requireSamples(t, samples, "aws_obj_head_total", "aws_obj_head_fails")
}
