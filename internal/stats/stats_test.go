package stats

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.k6.io/k6/js/modulestest"
	"go.k6.io/k6/lib"
	"go.k6.io/k6/metrics"
)

func TestReportDataSentPushesSample(t *testing.T) {
	rt := modulestest.NewRuntime(t)
	registry := metrics.NewRegistry()
	builtinMetrics := metrics.RegisterBuiltinMetrics(registry)
	samples := make(chan metrics.SampleContainer, 1)
	state := &lib.State{
		Samples:        samples,
		Tags:           lib.NewVUStateTags(registry.RootTagSet()),
		BuiltinMetrics: builtinMetrics,
	}
	rt.MoveToVUContext(state)

	ReportDataSent(rt.VU, 123)

	require.Len(t, samples, 1)
	got := (<-samples).GetSamples()
	require.Len(t, got, 1)
	require.Same(t, builtinMetrics.DataSent, got[0].Metric)
	require.Equal(t, metrics.DataSentName, got[0].Metric.Name)
	require.Equal(t, float64(123), got[0].Value)
}
