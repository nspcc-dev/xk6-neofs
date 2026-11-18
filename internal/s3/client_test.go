package s3

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/stretchr/testify/require"
	"go.k6.io/k6/js/modulestest"
	"go.k6.io/k6/lib"
	"go.k6.io/k6/metrics"
)

func newTestClient(endpoint string) *s3.Client {
	return s3.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("a", "b", ""),
	}, func(options *s3.Options) {
		options.BaseEndpoint = aws.String(endpoint)
		options.UsePathStyle = true
		options.Retryer = aws.NopRetryer{}
	})
}

// newVUTestClient returns a client of the endpoint whose VU sends its
// samples to the returned channel.
func newVUTestClient(t *testing.T, endpoint string) (*Client, chan metrics.SampleContainer) {
	t.Helper()

	rt := modulestest.NewRuntime(t)
	samples := make(chan metrics.SampleContainer, 16)
	// Module instances are created in the init context, before the VU runs.
	instance := new(RootModule).NewModuleInstance(rt.VU).(*S3)
	rt.MoveToVUContext(&lib.State{
		Samples: samples,
		Tags:    lib.NewVUStateTags(rt.VU.InitEnvField.Registry.RootTagSet()),
	})

	return &Client{vu: rt.VU, metrics: instance.metrics, cli: newTestClient(endpoint)}, samples
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

func TestGetReturnsErrorOnTruncatedBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "100")
		_, _ = w.Write([]byte("0123456789"))
	}))
	defer srv.Close()

	var got int
	err := get(context.Background(), newTestClient(srv.URL), "bucket", "key", func(chunk []byte) {
		got += len(chunk)
	})

	require.Error(t, err)
	require.ErrorContains(t, err, "read object body")
	require.Equal(t, 10, got)
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

	var got []byte
	err := get(context.Background(), newTestClient(srv.URL), "bucket", "key", func(chunk []byte) {
		got = append(got, chunk...)
	})

	require.NoError(t, err)
	require.Equal(t, payload, got)
}

func TestHeadReportsSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c, samples := newVUTestClient(t, srv.URL)

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

	c, samples := newVUTestClient(t, srv.URL)

	resp := c.Head("bucket", "key")
	require.False(t, resp.Success)
	require.NotEmpty(t, resp.Error)

	requireSamples(t, samples, "aws_obj_head_total", "aws_obj_head_fails")
}
