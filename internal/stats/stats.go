package stats

import (
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
