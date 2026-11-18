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

type testRequest struct {
	r        *Request
	samples  chan metrics.SampleContainer
	total    *metrics.Metric
	duration *metrics.Metric
	ttfb     *metrics.Metric
	fails    *metrics.Metric
}

func newTestRequest(t *testing.T) testRequest {
	t.Helper()

	rt := modulestest.NewRuntime(t)
	registry := metrics.NewRegistry()
	builtinMetrics := metrics.RegisterBuiltinMetrics(registry)
	samples := make(chan metrics.SampleContainer, 1)
	state := &lib.State{
		Samples:        samples,
		Tags:           lib.NewVUStateTags(registry.RootTagSet()),
		BuiltinMetrics: builtinMetrics,
	}
	state.Tags.Modify(func(tagsAndMeta *metrics.TagsAndMeta) {
		tagsAndMeta.SetMetadata("request", "test")
	})
	rt.MoveToVUContext(state)

	total := registry.MustNewMetric("test_total", metrics.Counter)
	return testRequest{
		r:        Start(rt.VU, total),
		samples:  samples,
		total:    total,
		duration: registry.MustNewMetric("test_duration", metrics.Trend, metrics.Time),
		ttfb:     registry.MustNewMetric("test_ttfb", metrics.Trend, metrics.Time),
		fails:    registry.MustNewMetric("test_fails", metrics.Rate),
	}
}

func TestRequestDonePushesOneConnectedContainer(t *testing.T) {
	tr := newTestRequest(t)
	tr.r.Add(tr.ttfb, metrics.D(10*time.Millisecond))
	tr.r.AddDataReceived(42)
	tr.r.Done(tr.duration)

	require.Len(t, tr.samples, 1)
	container, ok := (<-tr.samples).(metrics.ConnectedSamples)
	require.True(t, ok)
	require.Equal(t, container.Time, container.GetTime())
	require.Len(t, container.Samples, 4)

	got := make(map[string]float64, len(container.Samples))
	for _, sample := range container.Samples {
		require.Equal(t, container.Time, sample.Time)
		require.Equal(t, container.Tags, sample.Tags)
		require.Equal(t, map[string]string{"request": "test"}, sample.Metadata)
		got[sample.Metric.Name] = sample.Value
	}
	require.Equal(t, float64(1), got[tr.total.Name])
	require.Equal(t, metrics.D(10*time.Millisecond), got[tr.ttfb.Name])
	require.Equal(t, float64(42), got[metrics.DataReceivedName])
	require.Contains(t, got, tr.duration.Name)
	require.GreaterOrEqual(t, got[tr.duration.Name], float64(0))
}

func TestRequestFailPushesOneConnectedContainer(t *testing.T) {
	tr := newTestRequest(t)
	tr.r.Fail(tr.fails)

	require.Len(t, tr.samples, 1)
	container, ok := (<-tr.samples).(metrics.ConnectedSamples)
	require.True(t, ok)
	require.Len(t, container.Samples, 2)

	got := make(map[string]float64, len(container.Samples))
	for _, sample := range container.Samples {
		require.Equal(t, container.Time, sample.Time)
		require.Equal(t, container.Tags, sample.Tags)
		require.Equal(t, map[string]string{"request": "test"}, sample.Metadata)
		got[sample.Metric.Name] = sample.Value
	}
	require.Equal(t, float64(1), got[tr.total.Name])
	require.Equal(t, float64(1), got[tr.fails.Name])
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
