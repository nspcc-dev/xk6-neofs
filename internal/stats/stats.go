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

// Request collects the samples of one request and sends them to k6 together
// when the request ends: one container, one timestamp, the VU's tags.
type Request struct {
	vu      modules.VU
	start   time.Time
	samples []metrics.Sample
}

// Start begins a request; total is its *_total counter.
func Start(vu modules.VU, total *metrics.Metric) *Request {
	r := &Request{vu: vu, start: time.Now()}
	r.Add(total, 1)
	return r
}

// Add adds a sample of another metric of the request (e.g. TTFB).
func (r *Request) Add(metric *metrics.Metric, value float64) {
	r.samples = append(r.samples, metrics.Sample{
		TimeSeries: metrics.TimeSeries{Metric: metric},
		Value:      value,
	})
}

// AddDataSent adds k6's data_sent.
func (r *Request) AddDataSent(bytes int64) {
	r.Add(r.vu.State().BuiltinMetrics.DataSent, float64(bytes))
}

// AddDataReceived adds k6's data_received.
func (r *Request) AddDataReceived(bytes int64) {
	r.Add(r.vu.State().BuiltinMetrics.DataReceived, float64(bytes))
}

// Done ends a successful request: it adds the time since Start to duration and
// sends all samples.
func (r *Request) Done(duration *metrics.Metric) {
	r.Add(duration, metrics.D(time.Since(r.start)))
	r.push()
}

// Fail ends a failed request: it adds 1 to fails and sends all samples.
func (r *Request) Fail(fails *metrics.Metric) {
	r.Add(fails, 1)
	r.push()
}

func (r *Request) push() {
	tagsAndMeta := r.vu.State().Tags.GetCurrentValues()
	t := time.Now()
	for i := range r.samples {
		r.samples[i].Time = t
		r.samples[i].Tags = tagsAndMeta.Tags
		r.samples[i].Metadata = tagsAndMeta.Metadata
	}
	metrics.PushIfNotDone(r.vu.Context(), r.vu.State().Samples, metrics.ConnectedSamples{
		Samples: r.samples,
		Tags:    tagsAndMeta.Tags,
		Time:    t,
	})
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
