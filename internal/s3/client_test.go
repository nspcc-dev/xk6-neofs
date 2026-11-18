package s3

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/stretchr/testify/require"
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
