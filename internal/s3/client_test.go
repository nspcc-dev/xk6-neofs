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
