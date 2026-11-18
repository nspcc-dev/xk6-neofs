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

	req := stats.Start(c.vu, c.metrics.objPutTotal)

	_, err := c.cli.PutObject(c.vu.Context(), &s3.PutObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
		Body:   rdr,
	})
	if err != nil {
		req.Fail(c.metrics.objPutFails)
		return PutResponse{Success: false, Error: err.Error()}
	}

	req.AddDataSent(sz)
	req.Done(c.metrics.objPutDuration)
	return PutResponse{Success: true}
}

func (c *Client) Delete(bucket, key string) DeleteResponse {
	req := stats.Start(c.vu, c.metrics.objDeleteTotal)

	_, err := c.cli.DeleteObject(c.vu.Context(), &s3.DeleteObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		req.Fail(c.metrics.objDeleteFails)
		return DeleteResponse{Success: false, Error: err.Error()}
	}

	req.Done(c.metrics.objDeleteDuration)
	return DeleteResponse{Success: true}
}

func (c *Client) Get(bucket, key string) GetResponse {
	req := stats.Start(c.vu, c.metrics.objGetTotal)

	var objSize = 0
	ttfb, ttfbBody, err := get(c.vu.Context(), c.cli, bucket, key, func(chunk []byte) {
		objSize += len(chunk)
	})
	if err != nil {
		req.Fail(c.metrics.objGetFails)
		return GetResponse{Success: false, Error: err.Error()}
	}

	req.Add(c.metrics.objGetTTFB, metrics.D(ttfb))
	if ttfbBody > 0 {
		req.Add(c.metrics.objGetTTFBBody, metrics.D(ttfbBody))
	}
	req.AddDataReceived(int64(objSize))
	req.Done(c.metrics.objGetDuration)
	return GetResponse{Success: true}
}

func (c *Client) Head(bucket, key string) HeadResponse {
	req := stats.Start(c.vu, c.metrics.objHeadTotal)

	_, err := c.cli.HeadObject(c.vu.Context(), &s3.HeadObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		req.Fail(c.metrics.objHeadFails)
		return HeadResponse{Success: false, Error: err.Error()}
	}

	req.Done(c.metrics.objHeadDuration)
	return HeadResponse{Success: true}
}

// chunkWriter adapts an onDataChunk callback to io.Writer.
type chunkWriter func(chunk []byte)

func (w chunkWriter) Write(p []byte) (int, error) {
	w(p)
	return len(p), nil
}

func get(
	ctx context.Context,
	c *s3.Client,
	bucket string,
	key string,
	onDataChunk func(chunk []byte),
) (ttfb, ttfbBody time.Duration, err error) {
	start := time.Now()

	// The client does not retry (see Connect), so there is one response.
	ctx = httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{
		GotFirstResponseByte: func() { ttfb = time.Since(start) },
	})

	obj, err := c.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return 0, 0, err
	}
	defer obj.Body.Close()

	body := stats.FirstByte{W: chunkWriter(onDataChunk)}
	if _, err := io.CopyBuffer(&body, obj.Body, make([]byte, 32*1024)); err != nil {
		return 0, 0, fmt.Errorf("read object body: %w", err)
	}

	return ttfb, body.Since(start), nil
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

	req := stats.Start(c.vu, c.metrics.objListTotal)

	var count int
	paginator := s3.NewListObjectsV2Paginator(c.cli, input)
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(c.vu.Context())
		if err != nil {
			req.Fail(c.metrics.objListFails)
			return ListResponse{Success: false, Error: err.Error()}
		}
		count += len(page.Contents)
	}

	req.Done(c.metrics.objListDuration)
	return ListResponse{Success: true, Count: count}
}

func (c *Client) VerifyHash(bucket, key, expectedHash string) VerifyHashResponse {
	hasher := sha256.New()
	_, _, err := get(c.vu.Context(), c.cli, bucket, key, func(data []byte) {
		hasher.Write(data)
	})
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
	req := stats.Start(c.vu, c.metrics.createBucketTotal)

	var err error
	var lockEnabled bool
	if lockEnabledStr, ok := params["lock_enabled"]; ok {
		if lockEnabled, err = strconv.ParseBool(lockEnabledStr); err != nil {
			req.Fail(c.metrics.createBucketFails)
			return CreateBucketResponse{Success: false, Error: "invalid lock_enabled params"}
		}
	}

	var bucketConfiguration *types.CreateBucketConfiguration
	if locationConstraint, ok := params["location_constraint"]; ok {
		bucketConfiguration = &types.CreateBucketConfiguration{
			LocationConstraint: types.BucketLocationConstraint(locationConstraint),
		}
	}

	_, err = c.cli.CreateBucket(c.vu.Context(), &s3.CreateBucketInput{
		Bucket:                     aws.String(bucket),
		ACL:                        types.BucketCannedACL(params["acl"]),
		CreateBucketConfiguration:  bucketConfiguration,
		ObjectLockEnabledForBucket: &lockEnabled,
	})
	if err != nil {
		req.Fail(c.metrics.createBucketFails)
		return CreateBucketResponse{Success: false, Error: err.Error()}
	}

	req.Done(c.metrics.createBucketDuration)
	return CreateBucketResponse{Success: true}
}
