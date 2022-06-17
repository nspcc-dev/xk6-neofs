package s3

import (
	"fmt"

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
	vu modules.VU
}

// Ensure the interfaces are implemented correctly.
var (
	_ modules.Instance = &S3{}
	_ modules.Module   = &RootModule{}

	objPutTotal, objPutFails, objPutDuration                   *metrics.Metric
	objGetTotal, objGetFails, objGetDuration                   *metrics.Metric
	createBucketTotal, createBucketFails, createBucketDuration *metrics.Metric
)

func init() {
	modules.Register("k6/x/neofs/s3", new(RootModule))

}

// NewModuleInstance implements the modules.Module interface and returns
// a new instance for each VU.
func (r *RootModule) NewModuleInstance(vu modules.VU) modules.Instance {
	mi := &S3{vu: vu}
	return mi
}

// Exports implements the modules.Instance interface and returns the exports
// of the JS module.
func (s *S3) Exports() modules.Exports {
	return modules.Exports{Default: s}
}

func (s *S3) Connect(endpoint string) (*Client, error) {
	resolver := aws.EndpointResolverWithOptionsFunc(func(_, _ string, _ ...interface{}) (aws.Endpoint, error) {
		return aws.Endpoint{
			URL: endpoint,
		}, nil
	})

	cfg, err := config.LoadDefaultConfig(s.vu.Context(), config.WithEndpointResolverWithOptions(resolver))
	if err != nil {
		return nil, fmt.Errorf("configuration error: %w", err)
	}

	cli := s3.NewFromConfig(cfg, func(options *s3.Options) {
		// use 'domain/bucket/key' instead of default 'bucket.domain/key' scheme
		options.UsePathStyle = true
		// do not retry failed requests, by default client does up to 3 retry
		options.Retryer = aws.NopRetryer{}
	})

	// register metrics
	registry := metrics.NewRegistry()
	objPutTotal, _ = registry.NewMetric("aws_obj_put_total", metrics.Counter)
	objPutFails, _ = registry.NewMetric("aws_obj_put_fails", metrics.Counter)
	objPutDuration, _ = registry.NewMetric("aws_obj_put_duration", metrics.Trend, metrics.Time)

	objGetTotal, _ = registry.NewMetric("aws_obj_get_total", metrics.Counter)
	objGetFails, _ = registry.NewMetric("aws_obj_get_fails", metrics.Counter)
	objGetDuration, _ = registry.NewMetric("aws_obj_get_duration", metrics.Trend, metrics.Time)

	createBucketTotal, _ = registry.NewMetric("aws_create_bucket_total", metrics.Counter)
	createBucketFails, _ = registry.NewMetric("aws_create_bucket_fails", metrics.Counter)
	createBucketDuration, _ = registry.NewMetric("aws_create_bucket_duration", metrics.Trend, metrics.Time)

	return &Client{
		vu:  s.vu,
		cli: cli,
	}, nil
}
