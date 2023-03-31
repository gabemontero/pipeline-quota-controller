package events

import (
	"context"
	corev1 "k8s.io/api/core/v1"
	"strings"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

const (
	NamespaceLabel string = "namespace"
	LimitMemMetric string = "stonesoup_mem_limit_throttle_total"
	LimitCPUMetric string = "stonesoup_cpu_limit_throttle_total"
	ReqMemMetric   string = "stonesoup_mem_req_throttle_total"
	ReqCPUMetric   string = "stonesoup_cpu_req_throttle_total"
	PodCountMetric string = "stonesoup_pod_count_throttle_total"
)

var (
	limitMemDesc   *prometheus.Desc
	limitCPUDesc   *prometheus.Desc
	requestMemDesc *prometheus.Desc
	requestCPUDesc *prometheus.Desc
	podCountDesc   *prometheus.Desc

	//TODO possibly want a new CR to track data ??  One per NS

	//limitMemLock      sync.Mutex
	//limitMemMap       map[string]int64
	limitMemCounter *prometheus.CounterVec
	//limitCPULock      sync.Mutex
	//limitCPUMap       map[string]int64
	limitCPUCounter *prometheus.CounterVec
	//requestMemLock    sync.Mutex
	//requestMemMap     map[string]int64
	requestMemCounter *prometheus.CounterVec
	//requestCPULock    sync.Mutex
	//requestCPUMap     map[string]int64
	requestCPUCounter *prometheus.CounterVec
	//podCountLock      sync.Mutex
	//podCountMap       map[string]int64
	podCounter *prometheus.CounterVec

	registered = false
	sc         statsCollector
	regLock    = sync.Mutex{}

	eventController *Controller

	logger = ctrl.Log.WithName("metrics")
)

func InitPrometheus(client client.Client, c *Controller) {
	regLock.Lock()
	defer regLock.Unlock()

	if registered {
		return
	}

	eventController = c

	registered = true
	//limitMemLock = sync.Mutex{}
	//limitMemMap = map[string]int64{}
	//limitCPULock = sync.Mutex{}
	//limitCPUMap = map[string]int64{}
	//requestMemLock = sync.Mutex{}
	//requestMemMap = map[string]int64{}
	//requestCPULock = sync.Mutex{}
	//requestCPUMap = map[string]int64{}
	//podCountLock = sync.Mutex{}
	//podCountMap = map[string]int64{}

	labels := []string{NamespaceLabel}
	//limitMemCounter = prometheus.NewCounterVec(prometheus.CounterOpts{
	//	Name: LimitMemMetric,
	//	Help: "Number of total PipelineRuns throttled because of memory limit quota",
	//}, labels)
	limitMemDesc = prometheus.NewDesc(LimitMemMetric,
		"Number of total PipelineRuns throttled because of memory limit quota",
		labels,
		nil)

	//limitCPUCounter = prometheus.NewCounterVec(prometheus.CounterOpts{
	//	Name: LimitCPUMetric,
	//	Help: "Number of PipelineRuns throttled because of cpu limit quota",
	//}, labels)
	limitCPUDesc = prometheus.NewDesc(LimitCPUMetric,
		"Number of PipelineRuns throttled because of cpu limit quota",
		labels,
		nil,
	)
	//requestMemCounter = prometheus.NewCounterVec(prometheus.CounterOpts{
	//	Name: ReqMemMetric,
	//	Help: "Number of total PipelineRuns throttled because of memory request quota",
	//}, labels)
	requestMemDesc = prometheus.NewDesc(ReqMemMetric,
		"Number of total PipelineRuns throttled because of memory request quota",
		labels,
		nil)

	//requestCPUCounter = prometheus.NewCounterVec(prometheus.CounterOpts{
	//	Name: ReqCPUMetric,
	//	Help: "Number of PipelineRuns throttled because of cpu request quota",
	//}, labels)
	requestCPUDesc = prometheus.NewDesc(ReqCPUMetric,
		"Number of PipelineRuns throttled because of cpu request quota",
		labels,
		nil,
	)
	//podCounter = prometheus.NewCounterVec(prometheus.CounterOpts{
	//	Name: PodCountMetric,
	//	Help: "Number of PipelineRuns throttled because of pod count quota",
	//}, labels)
	podCountDesc = prometheus.NewDesc(PodCountMetric,
		"Number of PipelineRuns throttled because of pod count quota",
		labels,
		nil,
	)

	//metrics.Registry.MustRegister(limitMemCounter, limitCPUCounter, requestMemCounter, requestCPUCounter, podCounter)

	//TODO based on our openshift builds experience, we have talked about the notion of tracking adoption
	// of various stonesoup features (i.e. product mgmt is curious how much has feature X been used for the life of this cluster),
	// or even stonesoup "overall", based on PipelineRun Counts that are incremented
	// each time a PipelineRun comes through the reconciler for the first time (i.e. we label the PipelineRun as
	// part of bumping the metric so we only bump once), and then this metric is immune to PipelineRuns getting pruned.
	// i.e. newStat = prometheus.NewGaugeVec(...) and then newStat.Inc() if first time through
	// Conversely, for "devops" concerns, the collections of existing PipelineRuns is typically more of what is needed.

	sc = statsCollector{
		client: client,
	}

	metrics.Registry.MustRegister(&sc)
}

type statsCollector struct {
	client client.Client
}

func (sc *statsCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- limitMemDesc
	ch <- limitCPUDesc
}

func (sc *statsCollector) Collect(ch chan<- prometheus.Metric) {
	//TODO possible also get an event Lister from the controller for the namespaces with PRs an cross reference numbers
	// by listing events and categorizing
	eventController.eventInformerMap.Range(func(key, value any) bool {
		ns := key.(string)
		cmList := corev1.ConfigMapList{}
		opts := &client.ListOptions{Namespace: ns}
		if err := sc.client.List(context.Background(), &cmList, opts); err != nil {
			return true
		}
		var limitMem, limitCPU, requestMem, requestCPU, podCount = 0, 0, 0, 0, 0
		for _, cm := range cmList.Items {
			if cm.Data != nil {
				for item, _ := range cm.Data {
					switch {
					case strings.HasPrefix(item, string(corev1.ResourceLimitsMemory)):
						limitMem++
					case strings.HasPrefix(item, string(corev1.ResourceLimitsCPU)):
						limitCPU++
					case strings.HasPrefix(item, string(corev1.ResourceRequestsMemory)):
						requestMem++
					case strings.HasPrefix(item, string(corev1.ResourceRequestsCPU)):
						requestCPU++
					case strings.HasPrefix(item, string(corev1.ResourcePods)):
						podCount++
					}

				}
			}
		}
		ch <- prometheus.MustNewConstMetric(limitMemDesc, prometheus.GaugeValue, float64(limitMem), ns)
		ch <- prometheus.MustNewConstMetric(limitCPUDesc, prometheus.GaugeValue, float64(limitCPU), ns)
		ch <- prometheus.MustNewConstMetric(requestMemDesc, prometheus.GaugeValue, float64(requestMem), ns)
		ch <- prometheus.MustNewConstMetric(requestCPUDesc, prometheus.GaugeValue, float64(requestCPU), ns)
		ch <- prometheus.MustNewConstMetric(podCountDesc, prometheus.GaugeValue, float64(podCount), ns)
		return true
	})

	//collectCounter(limitMemLock, limitMemMap, ch, limitMemDesc)
	//collectCounter(limitCPULock, limitCPUMap, ch, limitCPUDesc)
	//collectCounter(requestMemLock, requestMemMap, ch, requestMemDesc)
	//collectCounter(requestCPULock, requestCPUMap, ch, requestCPUDesc)
	//collectCounter(podCountLock, podCountMap, ch, podCountDesc)
}

//func collectCounter(lock sync.Mutex, countMap map[string]int64, ch chan<- prometheus.Metric, desc *prometheus.Desc) {
//	lock.Lock()
//	defer lock.Unlock()
//	for ns, count := range countMap {
//		ch <- prometheus.MustNewConstMetric(desc, prometheus.GaugeValue, float64(count), ns)
//	}
//}
//
//func bumpCount(lock sync.Mutex, countMap map[string]int64, ns string) {
//	lock.Lock()
//	defer lock.Unlock()
//	count, ok := countMap[ns]
//	if !ok {
//		countMap[ns] = 1
//	} else {
//		count++
//		countMap[ns] = count
//	}
//}
