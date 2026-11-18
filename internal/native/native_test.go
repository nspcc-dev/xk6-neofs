package native

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.k6.io/k6/js/modulestest"
	"go.k6.io/k6/metrics"
)

func TestNativeMetricsRegisteredInInitRegistry(t *testing.T) {
	rt := modulestest.NewRuntime(t)
	registry := rt.VU.InitEnvField.Registry

	first := new(RootModule).NewModuleInstance(rt.VU).(*Native).metrics
	second := new(RootModule).NewModuleInstance(rt.VU).(*Native).metrics

	firstMetrics := nativeMetricMap(first)
	secondMetrics := nativeMetricMap(second)
	for name, metric := range firstMetrics {
		registered := registry.Get(name)
		require.NotNil(t, registered, name)
		require.Same(t, registered, metric, name)
		require.Same(t, metric, secondMetrics[name], name)
	}
}

func nativeMetricMap(ms *metricSet) map[string]*metrics.Metric {
	return map[string]*metrics.Metric{
		"neofs_obj_put_total":            ms.objPutTotal,
		"neofs_obj_put_fails":            ms.objPutFails,
		"neofs_obj_put_duration":         ms.objPutDuration,
		"neofs_obj_get_total":            ms.objGetTotal,
		"neofs_obj_get_fails":            ms.objGetFails,
		"neofs_obj_get_duration":         ms.objGetDuration,
		"neofs_obj_head_total":           ms.objHeadTotal,
		"neofs_obj_head_fails":           ms.objHeadFails,
		"neofs_obj_head_duration":        ms.objHeadDuration,
		"neofs_obj_list_total":           ms.objListTotal,
		"neofs_obj_list_fails":           ms.objListFails,
		"neofs_obj_list_duration":        ms.objListDuration,
		"neofs_obj_delete_total":         ms.objDeleteTotal,
		"neofs_obj_delete_fails":         ms.objDeleteFails,
		"neofs_obj_delete_duration":      ms.objDeleteDuration,
		"neofs_cnr_put_total":            ms.cnrPutTotal,
		"neofs_cnr_put_fails":            ms.cnrPutFails,
		"neofs_cnr_put_duration":         ms.cnrPutDuration,
		"neofs_search_duration_relative": ms.objSearchDurationRelative,
	}
}
