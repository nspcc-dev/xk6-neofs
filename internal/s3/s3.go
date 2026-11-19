package s3

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"go.k6.io/k6/js/modules"
	"go.k6.io/k6/metrics"
)

// RootModule is the global module object type. It is instantiated once per test
// run and will be used to create k6/x/neofs/s3 module instances for each VU.
type RootModule struct{}

// S3 represents an instance of the module for every VU.
type S3 struct {
	vu      modules.VU
	metrics *metricSet
}

type metricSet struct {
	objPutTotal, objPutFails, objPutDuration                   *metrics.Metric
	objGetTotal, objGetFails, objGetDuration, objGetTTFB       *metrics.Metric
	objGetTTFBBody                                             *metrics.Metric
	objHeadTotal, objHeadFails, objHeadDuration                *metrics.Metric
	objListTotal, objListFails, objListDuration                *metrics.Metric
	objDeleteTotal, objDeleteFails, objDeleteDuration          *metrics.Metric
	createBucketTotal, createBucketFails, createBucketDuration *metrics.Metric
}

func newMetricSet(registry *metrics.Registry) *metricSet {
	return &metricSet{
		objPutTotal:          registry.MustNewMetric("aws_obj_put_total", metrics.Counter),
		objPutFails:          registry.MustNewMetric("aws_obj_put_fails", metrics.Counter),
		objPutDuration:       registry.MustNewMetric("aws_obj_put_duration", metrics.Trend, metrics.Time),
		objGetTotal:          registry.MustNewMetric("aws_obj_get_total", metrics.Counter),
		objGetFails:          registry.MustNewMetric("aws_obj_get_fails", metrics.Counter),
		objGetDuration:       registry.MustNewMetric("aws_obj_get_duration", metrics.Trend, metrics.Time),
		objGetTTFB:           registry.MustNewMetric("aws_obj_get_ttfb", metrics.Trend, metrics.Time),
		objGetTTFBBody:       registry.MustNewMetric("aws_obj_get_ttfb_body", metrics.Trend, metrics.Time),
		objHeadTotal:         registry.MustNewMetric("aws_obj_head_total", metrics.Counter),
		objHeadFails:         registry.MustNewMetric("aws_obj_head_fails", metrics.Counter),
		objHeadDuration:      registry.MustNewMetric("aws_obj_head_duration", metrics.Trend, metrics.Time),
		objListTotal:         registry.MustNewMetric("aws_obj_list_total", metrics.Counter),
		objListFails:         registry.MustNewMetric("aws_obj_list_fails", metrics.Counter),
		objListDuration:      registry.MustNewMetric("aws_obj_list_duration", metrics.Trend, metrics.Time),
		objDeleteTotal:       registry.MustNewMetric("aws_obj_delete_total", metrics.Counter),
		objDeleteFails:       registry.MustNewMetric("aws_obj_delete_fails", metrics.Counter),
		objDeleteDuration:    registry.MustNewMetric("aws_obj_delete_duration", metrics.Trend, metrics.Time),
		createBucketTotal:    registry.MustNewMetric("aws_create_bucket_total", metrics.Counter),
		createBucketFails:    registry.MustNewMetric("aws_create_bucket_fails", metrics.Counter),
		createBucketDuration: registry.MustNewMetric("aws_create_bucket_duration", metrics.Trend, metrics.Time),
	}
}

// Ensure the interfaces are implemented correctly.
var (
	_ modules.Instance = &S3{}
	_ modules.Module   = &RootModule{}
)

const defaultAWSRegion = "us-east-1"

func init() {
	modules.Register("k6/x/neofs/s3", new(RootModule))
}

// NewModuleInstance implements the modules.Module interface and returns
// a new instance for each VU.
func (r *RootModule) NewModuleInstance(vu modules.VU) modules.Instance {
	return &S3{
		vu:      vu,
		metrics: newMetricSet(vu.InitEnv().Registry),
	}
}

// Exports implements the modules.Instance interface and returns the exports
// of the JS module.
func (s *S3) Exports() modules.Exports {
	return modules.Exports{Default: s}
}

func loadDefaultAWSConfig(ctx context.Context) (aws.Config, error) {
	return config.LoadDefaultConfig(ctx, config.WithDefaultRegion(defaultAWSRegion))
}

func (s *S3) Connect(endpoint string, params map[string]string) (*Client, error) {
	cfg, err := loadDefaultAWSConfig(s.vu.Context())
	if err != nil {
		return nil, fmt.Errorf("configuration error: %w", err)
	}

	var noVerifySSL bool
	if noVerifySSLStr, ok := params["no_verify_ssl"]; ok {
		if noVerifySSL, err = strconv.ParseBool(noVerifySSLStr); err != nil {
			return nil, fmt.Errorf("invalid value for 'no_verify_ssl': '%s'", noVerifySSLStr)
		}
	}

	var timeout time.Duration
	if timeoutStr, ok := params["timeout"]; ok {
		if timeout, err = time.ParseDuration(timeoutStr); err != nil {
			return nil, fmt.Errorf("invalid value for 'timeout': '%s'", timeoutStr)
		}
	}

	cli := s3.NewFromConfig(cfg, func(options *s3.Options) {
		options.DisableLogOutputChecksumValidationSkipped = true
		options.BaseEndpoint = aws.String(endpoint)
		// use 'domain/bucket/key' instead of default 'bucket.domain/key' scheme
		options.UsePathStyle = true
		// do not retry failed requests, by default client does up to 3 retry
		options.Retryer = aws.NopRetryer{}
		// s3 sometimes use self-signed certs
		options.HTTPClient = &http.Client{
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					InsecureSkipVerify: noVerifySSL,
				},
			},
			Timeout: timeout,
		}
	})

	return &Client{
		vu:      s.vu,
		metrics: s.metrics,
		cli:     cli,
	}, nil
}
