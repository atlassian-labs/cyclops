package observer

import (
	"reflect"

	"github.com/prometheus/client_golang/prometheus"

	"k8s.io/klog/v2"
)

const metricsNamespace = "cyclops_observer"

// metrics struct contains all the prometheus metrics for the controller
type metrics struct {
	NodeGroupsOutOfDate *prometheus.CounterVec
	CNRsCreated         *prometheus.CounterVec
	NodeGroupsLocked    *prometheus.CounterVec
	ObserverRunTimes    *prometheus.GaugeVec
	NodeGroupChangeStatus *prometheus.GaugeVec
}

// newMetrics creates the new controller metrics struct
func newMetrics() *metrics {
	return &metrics{
		NodeGroupsOutOfDate: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name:      "node_groups_out_of_date",
				Namespace: metricsNamespace,
				Help:      "counter of nodegroups found out of date changed",
			},
			[]string{"observer"},
		),
		CNRsCreated: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name:      "cnrs_created",
				Namespace: metricsNamespace,
				Help:      "counter of cnrs created by observer",
			},
			[]string{"nodegroup"},
		),
		NodeGroupsLocked: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name:      "nodegroups_locked",
				Namespace: metricsNamespace,
				Help:      "counter of nodegroups locked when checking changes",
			},
			[]string{"nodegroup"},
		),
		ObserverRunTimes: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name:      "run_times",
				Namespace: metricsNamespace,
				Help:      "gauge of observer runtimes in seconds",
			},
			[]string{"observer"},
		),

		NodeGroupChangeStatus: prometheus.NewGaugeVec(
            prometheus.GaugeOpts{
                Name:      "node_group_change_status",
                Namespace: metricsNamespace,
                Help:      "current change status of nodegroups (0=up to date, 1=out of date)",
            },
            []string{"nodegroup_name"},
        ),

	}
}

// collectMetricsStruct uses magic (reflection) to automatically fill prometheus with the Metrics from a struct
func collectMetricsStruct(v interface{}) {
	if c, ok := v.(prometheus.Collector); ok {
		prometheus.MustRegister(c)
	}

	val := reflect.ValueOf(v).Elem()

	for i := 0; i < val.NumField(); i++ {
		field := val.Field(i)

		if !field.CanInterface() {
			continue
		}

		collector, ok := field.Interface().(prometheus.Collector)
		if !ok {
			continue
		}

		klog.V(5).Infoln("registering collector", val.Type().Field(i).Name, "as metric")
		prometheus.MustRegister(collector)
	}
}
