package s3

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http/httptrace"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/grafana/sobek"
	"github.com/nspcc-dev/xk6-neofs/internal/stats"
	"go.k6.io/k6/js/modules"
	"go.k6.io/k6/metrics"
)

type (
	Client struct {
		vu      modules.VU
		metrics *metricSet
		cli     *s3.Client
	}

	PutResponse struct {
		Success bool
		Error   string
	}

	DeleteResponse struct {
		Success bool
		Error   string
	}

	GetResponse struct {
		Success bool
		Error   string
	}

	HeadResponse struct {
		Success bool
		Error   string
	}

	ListResponse struct {
		Success bool
		Error   string
		Count   int
	}

	CreateBucketResponse struct {
		Success bool
		Error   string
	}

	VerifyHashResponse struct {
		Success bool
		Error   string
	}
)

func (c *Client) Put(bucket, key string, payload sobek.ArrayBuffer) PutResponse {
	rdr := bytes.NewReader(payload.Bytes())
	sz := rdr.Size()

	stats.Report(c.vu, c.metrics.objPutTotal, 1)

	start := time.Now()
	_, err := c.cli.PutObject(c.vu.Context(), &s3.PutObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
		Body:   rdr,
	})
	if err != nil {
		stats.Report(c.vu, c.metrics.objPutFails, 1)
		return PutResponse{Success: false, Error: err.Error()}
	}

	stats.ReportDataSent(c.vu, float64(sz))
	stats.Report(c.vu, c.metrics.objPutDuration, metrics.D(time.Since(start)))
	return PutResponse{Success: true}
}

func (c *Client) Delete(bucket, key string) DeleteResponse {
	stats.Report(c.vu, c.metrics.objDeleteTotal, 1)
	start := time.Now()

	_, err := c.cli.DeleteObject(c.vu.Context(), &s3.DeleteObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		stats.Report(c.vu, c.metrics.objDeleteFails, 1)
		return DeleteResponse{Success: false, Error: err.Error()}
	}

	stats.Report(c.vu, c.metrics.objDeleteDuration, metrics.D(time.Since(start)))
	return DeleteResponse{Success: true}
}

func (c *Client) Get(bucket, key string) GetResponse {
	stats.Report(c.vu, c.metrics.objGetTotal, 1)
	start := time.Now()

	objSize, ttfb, ttfbBody, err := get(c.vu.Context(), c.cli, bucket, key, io.Discard)
	if err != nil {
		stats.Report(c.vu, c.metrics.objGetFails, 1)
		return GetResponse{Success: false, Error: err.Error()}
	}

	stats.Report(c.vu, c.metrics.objGetDuration, metrics.D(time.Since(start)))
	stats.Report(c.vu, c.metrics.objGetTTFB, metrics.D(ttfb))
	if ttfbBody > 0 {
		stats.Report(c.vu, c.metrics.objGetTTFBBody, metrics.D(ttfbBody))
	}
	stats.ReportDataReceived(c.vu, float64(objSize))
	return GetResponse{Success: true}
}

func (c *Client) Head(bucket, key string) HeadResponse {
	stats.Report(c.vu, c.metrics.objHeadTotal, 1)
	start := time.Now()

	_, err := c.cli.HeadObject(c.vu.Context(), &s3.HeadObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		stats.Report(c.vu, c.metrics.objHeadFails, 1)
		return HeadResponse{Success: false, Error: err.Error()}
	}

	stats.Report(c.vu, c.metrics.objHeadDuration, metrics.D(time.Since(start)))
	return HeadResponse{Success: true}
}

func get(
	ctx context.Context,
	c *s3.Client,
	bucket string,
	key string,
	w io.Writer,
) (int64, time.Duration, time.Duration, error) {
	start := time.Now()

	var firstByte atomic.Int64
	ctx = httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{
		GotFirstResponseByte: func() {
			firstByte.CompareAndSwap(0, int64(time.Since(start)))
		},
	})

	obj, err := c.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return 0, 0, 0, err
	}
	defer obj.Body.Close()

	body := stats.FirstByte{W: w}
	n, err := io.Copy(&body, obj.Body)
	if err != nil {
		return n, 0, 0, fmt.Errorf("read object body: %w", err)
	}

	return n, time.Duration(firstByte.Load()), body.Since(start), nil
}

// List lists all objects in the bucket that match the prefix. It follows all
// pages of the listing, so one call is one complete listing.
//
// params may set fetch_owner to "true" to ask for the owner of every object,
// as minio-go clients (warp among them) always do. It is not free: the
// gateway has to look the owners up.
func (c *Client) List(bucket, prefix string, params map[string]string) ListResponse {
	input := &s3.ListObjectsV2Input{Bucket: aws.String(bucket)}
	if prefix != "" {
		input.Prefix = aws.String(prefix)
	}
	if v, ok := params["fetch_owner"]; ok {
		fetchOwner, err := strconv.ParseBool(v)
		if err != nil {
			return ListResponse{Success: false, Error: fmt.Sprintf("invalid fetch_owner param: %v", err)}
		}
		input.FetchOwner = aws.Bool(fetchOwner)
	}

	stats.Report(c.vu, c.metrics.objListTotal, 1)
	start := time.Now()

	var count int
	paginator := s3.NewListObjectsV2Paginator(c.cli, input)
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(c.vu.Context())
		if err != nil {
			stats.Report(c.vu, c.metrics.objListFails, 1)
			return ListResponse{Success: false, Error: err.Error()}
		}
		count += len(page.Contents)
	}

	stats.Report(c.vu, c.metrics.objListDuration, metrics.D(time.Since(start)))
	return ListResponse{Success: true, Count: count}
}

func (c *Client) VerifyHash(bucket, key, expectedHash string) VerifyHashResponse {
	hasher := sha256.New()
	_, _, _, err := get(c.vu.Context(), c.cli, bucket, key, hasher)
	if err != nil {
		return VerifyHashResponse{Success: false, Error: err.Error()}
	}
	actualHash := hex.EncodeToString(hasher.Sum(nil))
	if actualHash != expectedHash {
		return VerifyHashResponse{Success: true, Error: "hash mismatch"}
	}

	return VerifyHashResponse{Success: true}
}

func (c *Client) CreateBucket(bucket string, params map[string]string) CreateBucketResponse {
	stats.Report(c.vu, c.metrics.createBucketTotal, 1)

	var err error
	var lockEnabled bool
	if lockEnabledStr, ok := params["lock_enabled"]; ok {
		if lockEnabled, err = strconv.ParseBool(lockEnabledStr); err != nil {
			stats.Report(c.vu, c.metrics.createBucketFails, 1)
			return CreateBucketResponse{Success: false, Error: "invalid lock_enabled params"}
		}
	}

	var bucketConfiguration *types.CreateBucketConfiguration
	if locationConstraint, ok := params["location_constraint"]; ok {
		bucketConfiguration = &types.CreateBucketConfiguration{
			LocationConstraint: types.BucketLocationConstraint(locationConstraint),
		}
	}

	start := time.Now()
	_, err = c.cli.CreateBucket(c.vu.Context(), &s3.CreateBucketInput{
		Bucket:                     aws.String(bucket),
		ACL:                        types.BucketCannedACL(params["acl"]),
		CreateBucketConfiguration:  bucketConfiguration,
		ObjectLockEnabledForBucket: &lockEnabled,
	})
	if err != nil {
		stats.Report(c.vu, c.metrics.createBucketFails, 1)
		return CreateBucketResponse{Success: false, Error: err.Error()}
	}

	stats.Report(c.vu, c.metrics.createBucketDuration, metrics.D(time.Since(start)))
	return CreateBucketResponse{Success: true}
}
