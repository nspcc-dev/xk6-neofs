package stats

import (
	"io"
	"time"

	"go.k6.io/k6/js/modules"
	"go.k6.io/k6/metrics"
)

// Report pushes one sample of the metric, tagged with the VU's current
// tags, to k6. Samples go through the VU's sample channel, so that k6
// aggregates them and every output (JSON, CSV, InfluxDB, ...) sees them.
func Report(vu modules.VU, metric *metrics.Metric, value float64) {
	state := vu.State()
	metrics.PushIfNotDone(vu.Context(), state.Samples, metrics.Sample{
		TimeSeries: metrics.TimeSeries{
			Metric: metric,
			Tags:   state.Tags.GetCurrentValues().Tags,
		},
		Time:  time.Now(),
		Value: value,
	})
}

// ReportDataReceived reports payload bytes received as k6's data_received.
func ReportDataReceived(vu modules.VU, value float64) {
	Report(vu, vu.State().BuiltinMetrics.DataReceived, value)
}

// ReportDataSent reports payload bytes sent as k6's data_sent.
func ReportDataSent(vu modules.VU, value float64) {
	Report(vu, vu.State().BuiltinMetrics.DataSent, value)
}

// FirstByte passes writes on to W and notes when the first byte went through.
type FirstByte struct {
	W io.Writer
	// At is when the first non-empty Write was made; zero if there was none.
	At time.Time
}

func (x *FirstByte) Write(p []byte) (int, error) {
	if x.At.IsZero() && len(p) > 0 {
		x.At = time.Now()
	}
	return x.W.Write(p)
}

// Since returns the time from start to the first byte, or zero if nothing was
// written.
func (x *FirstByte) Since(start time.Time) time.Duration {
	if x.At.IsZero() {
		return 0
	}
	return x.At.Sub(start)
}
