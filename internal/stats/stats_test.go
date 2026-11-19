package stats

import (
	"bytes"
	"testing"
	"time"

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

func TestFirstByte(t *testing.T) {
	start := time.Now()
	var buf bytes.Buffer
	w := FirstByte{W: &buf}
	require.Zero(t, w.Since(start))

	// An empty write is not a byte.
	_, err := w.Write(nil)
	require.NoError(t, err)
	require.Zero(t, w.Since(start))

	_, err = w.Write([]byte("ab"))
	require.NoError(t, err)
	first := w.Since(start)
	require.Positive(t, first)

	// Later writes do not move it, and everything is passed on.
	time.Sleep(time.Millisecond)
	_, err = w.Write([]byte("c"))
	require.NoError(t, err)
	require.Equal(t, first, w.Since(start))
	require.Equal(t, "abc", buf.String())
}
