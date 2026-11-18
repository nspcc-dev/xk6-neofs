package s3

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.k6.io/k6/js/modulestest"
	"go.k6.io/k6/metrics"
)

func TestS3MetricsRegisteredInInitRegistry(t *testing.T) {
	rt := modulestest.NewRuntime(t)
	registry := rt.VU.InitEnvField.Registry

	first := new(RootModule).NewModuleInstance(rt.VU).(*S3).metrics
	second := new(RootModule).NewModuleInstance(rt.VU).(*S3).metrics

	firstMetrics := s3MetricMap(first)
	secondMetrics := s3MetricMap(second)
	for name, metric := range firstMetrics {
		registered := registry.Get(name)
		require.NotNil(t, registered, name)
		require.Same(t, registered, metric, name)
		require.Same(t, metric, secondMetrics[name], name)
	}
}

func s3MetricMap(ms *metricSet) map[string]*metrics.Metric {
	return map[string]*metrics.Metric{
		"aws_obj_put_total":          ms.objPutTotal,
		"aws_obj_put_fails":          ms.objPutFails,
		"aws_obj_put_duration":       ms.objPutDuration,
		"aws_obj_get_total":          ms.objGetTotal,
		"aws_obj_get_fails":          ms.objGetFails,
		"aws_obj_get_duration":       ms.objGetDuration,
		"aws_obj_head_total":         ms.objHeadTotal,
		"aws_obj_head_fails":         ms.objHeadFails,
		"aws_obj_head_duration":      ms.objHeadDuration,
		"aws_obj_delete_total":       ms.objDeleteTotal,
		"aws_obj_delete_fails":       ms.objDeleteFails,
		"aws_obj_delete_duration":    ms.objDeleteDuration,
		"aws_create_bucket_total":    ms.createBucketTotal,
		"aws_create_bucket_fails":    ms.createBucketFails,
		"aws_create_bucket_duration": ms.createBucketDuration,
	}
}
