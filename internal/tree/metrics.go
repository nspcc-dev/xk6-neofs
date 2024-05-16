package tree

import (
	"go.k6.io/k6/js/modules"
	"go.k6.io/k6/metrics"
)

type treeMetrics struct {
	ErrRate *metrics.Metric

	AddTotal    *metrics.Metric
	AddFails    *metrics.Metric
	AddDuration *metrics.Metric

	AddByPathTotal    *metrics.Metric
	AddByPathFails    *metrics.Metric
	AddByPathDuration *metrics.Metric
}

func registerMetrics(vu modules.VU) (treeMetrics, error) {
	var err error
	registry := vu.InitEnv().Registry
	m := treeMetrics{}

	m.ErrRate, err = registry.NewMetric("tree_err_rate", metrics.Rate)
	if err != nil {
		return m, err
	}

	// ADD

	m.AddTotal, err = registry.NewMetric("tree_add_total", metrics.Counter)
	if err != nil {
		return m, err
	}
	m.AddFails, err = registry.NewMetric("tree_add_fail", metrics.Counter)
	if err != nil {
		return m, err
	}
	m.AddDuration, err = registry.NewMetric("tree_add_duration", metrics.Trend, metrics.Time)
	if err != nil {
		return m, err
	}

	// ADD_BY_PATH

	m.AddByPathTotal, err = registry.NewMetric("tree_add_by_path_total", metrics.Counter)
	if err != nil {
		return m, err
	}
	m.AddByPathFails, err = registry.NewMetric("tree_add_by_path_fail", metrics.Counter)
	if err != nil {
		return m, err
	}
	m.AddByPathDuration, err = registry.NewMetric("tree_add_by_path_duration", metrics.Trend, metrics.Time)
	if err != nil {
		return m, err
	}

	return m, nil
}
