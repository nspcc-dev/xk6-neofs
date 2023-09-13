package stats

import (
	"time"

	"go.k6.io/k6/js/modules"
	"go.k6.io/k6/metrics"
)

func Report(vu modules.VU, metric *metrics.Metric, value float64) {
	metrics.PushIfNotDone(vu.Context(), vu.State().Samples, metrics.Sample{
		TimeSeries: metrics.TimeSeries{
			Metric: metric,
		},
		Time:  time.Now(),
		Value: value,
	})
}

func ReportDataReceived(vu modules.VU, value float64) {
	vu.State().BuiltinMetrics.DataReceived.Sink.Add(
		metrics.Sample{
			Value: value,
			Time:  time.Now()},
	)
}

func ReportDataSent(vu modules.VU, value float64) {
	state := vu.State()
	state.BuiltinMetrics.DataSent.Sink.Add(
		metrics.Sample{
			Value: value,
			Time:  time.Now()},
	)
}
