package s3

import (
	"bytes"
	"context"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/dop251/goja"
	"github.com/nspcc-dev/xk6-neofs/internal/stats"
	"go.k6.io/k6/js/modules"
	"go.k6.io/k6/metrics"
)

type (
	Client struct {
		vu  modules.VU
		cli *s3.Client
	}

	PutResponse struct {
		Success bool
		Error   string
	}

	GetResponse struct {
		Success bool
		Error   string
	}
)

func (c *Client) Put(bucket, key string, payload goja.ArrayBuffer) PutResponse {
	rdr := bytes.NewReader(payload.Bytes())
	sz := rdr.Size()

	stats.Report(c.vu, objPutTotal, 1)

	start := time.Now()
	_, err := c.cli.PutObject(c.vu.Context(), &s3.PutObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
		Body:   rdr,
	})
	if err != nil {
		stats.Report(c.vu, objPutFails, 1)
		return PutResponse{Success: false, Error: err.Error()}
	}

	stats.ReportDataSent(c.vu, float64(sz))
	stats.Report(c.vu, objPutDuration, metrics.D(time.Since(start)))
	return PutResponse{Success: true}
}

func (c *Client) Get(bucket, key string) GetResponse {
	var (
		buf = make([]byte, 4*1024)
		sz  int
	)
	stats.Report(c.vu, objGetTotal, 1)
	start := time.Now()
	obj, err := c.cli.GetObject(context.Background(), &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		stats.Report(c.vu, objGetFails, 1)
		return GetResponse{Success: false, Error: err.Error()}
	}
	stats.Report(c.vu, objGetDuration, metrics.D(time.Since(start)))
	for {
		n, err := obj.Body.Read(buf)
		if n > 0 {
			sz += n
		}
		if err != nil {
			break
		}
	}
	stats.ReportDataReceived(c.vu, float64(sz))
	return GetResponse{Success: true}
}
