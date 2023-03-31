package pipelinerun

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/tektoncd/pipeline/pkg/apis/pipeline/v1beta1"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"knative.dev/pkg/apis"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

const (
	NamespaceLabel     string = "namespace"
	LimitMemMetric     string = "stonesoup_mem_limit_throttle_total"
	LimitCPUMetric     string = "stonesoup_cpu_limit_throttle_total"
	ReqMemMetric       string = "stonesoup_mem_req_throttle_total"
	ReqCPUMetric       string = "stonesoup_cpu_req_throttle_total"
	PodCountMetric     string = "stonesoup_pod_count_throttle_total"
	NodeResourceMetric string = "stonesoup_node_resources_throttle_total"
)

var (
	limitMemDesc     *prometheus.Desc
	limitCPUDesc     *prometheus.Desc
	requestMemDesc   *prometheus.Desc
	requestCPUDesc   *prometheus.Desc
	podCountDesc     *prometheus.Desc
	nodeResourceDesc *prometheus.Desc

	registered = false
	sc         statsCollector
	regLock    = sync.Mutex{}

	logger = ctrl.Log.WithName("metrics")
)

func InitPrometheus(client client.Client) {
	regLock.Lock()
	defer regLock.Unlock()

	if registered {
		return
	}

	registered = true

	labels := []string{NamespaceLabel}
	limitMemDesc = prometheus.NewDesc(LimitMemMetric,
		"Number of total PipelineRuns throttled because of memory limit quota",
		labels,
		nil)

	limitCPUDesc = prometheus.NewDesc(LimitCPUMetric,
		"Number of PipelineRuns throttled because of cpu limit quota",
		labels,
		nil,
	)
	requestMemDesc = prometheus.NewDesc(ReqMemMetric,
		"Number of total PipelineRuns throttled because of memory request quota",
		labels,
		nil)

	requestCPUDesc = prometheus.NewDesc(ReqCPUMetric,
		"Number of PipelineRuns throttled because of cpu request quota",
		labels,
		nil,
	)
	podCountDesc = prometheus.NewDesc(PodCountMetric,
		"Number of PipelineRuns throttled because of pod count quota",
		labels,
		nil,
	)
	nodeResourceDesc = prometheus.NewDesc(NodeResourceMetric,
		"Number of PipelineRuns throttled because of overall node resource consumption",
		labels,
		nil)

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
	ch <- requestMemDesc
	ch <- requestCPUDesc
	ch <- podCountDesc
	ch <- nodeResourceDesc
}

func (sc *statsCollector) Collect(ch chan<- prometheus.Metric) {
	//TODO possible also get an event Lister from the controller for the namespaces with PRs an cross reference numbers
	// by listing events and categorizing
	prList := v1beta1.PipelineRunList{}
	opts := &client.ListOptions{Namespace: metav1.NamespaceAll}
	if err := sc.client.List(context.Background(), &prList, opts); err != nil {
		logger.Error(err, "error listing PipelineRuns")
		return
	}
	limitMemMap := map[string]int{}
	limitCPUMap := map[string]int{}
	reqMemMap := map[string]int{}
	reqCPUMap := map[string]int{}
	podCountMap := map[string]int{}
	nodeResourceMap := map[string]int{}
	for _, pr := range prList.Items {
		if pr.IsDone() || pr.IsCancelled() {
			continue
		}
		var limitMem, limitCPU, requestMem, requestCPU, podCount, nodeResource = 0, 0, 0, 0, 0, 0
		ns := pr.Namespace
		limitMem = limitMemMap[ns]
		limitCPU = limitCPUMap[ns]
		requestMem = reqMemMap[ns]
		requestCPU = reqCPUMap[ns]
		podCount = podCountMap[ns]
		nodeResource = nodeResourceMap[ns]
		for _, taskRunStatus := range pr.Status.TaskRuns {
			// key ^^ is task run name
			if taskRunStatus != nil {
				succeedCondition := taskRunStatus.Status.GetCondition(apis.ConditionSucceeded)
				if succeedCondition != nil && succeedCondition.Status == corev1.ConditionUnknown {
					switch succeedCondition.Reason {
					// no k8s constants for this; there are tekton ones in pkg/pod but that brings in internal packages that monkey with go mod
					case "ExceededResourceQuota":
						/*
							message: 'TaskRun Pod exceeded available resources: pods "fc4a2449870a0f7119c98ebb771942fe-build-0-task-pod"
							              is forbidden: exceeded quota: compute-build, requested: requests.memory=3Gi,
							              used: requests.memory=63Gi, limited: requests.memory=64Gi'
						*/
						seg1 := strings.Split(succeedCondition.Message, "exceeded quota")
						if len(seg1) < 2 {
							logger.Info(fmt.Sprintf("unexpected stage 1 ExceededResourceQuota msg %s", succeedCondition.Message))
							continue
						}
						seg2 := strings.Split(seg1[1], ",")
						if len(seg2) < 4 {
							logger.Info(fmt.Sprintf("unexpected stage 2 ExceededResourceQuota msg %s", succeedCondition.Message))
							continue
						}
						//quotaName := strings.TrimSpace(seg2[0])
						//requestedUnit := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(seg2[1]), "requested:"))
						//usedUnit := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(seg2[2]), "used:"))
						limitUnit := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(seg2[3]), "limited:"))

						// there can be more than one limit reason, i.e. limited: limits.memory=100Gi,requests.memory=32Gi
						individualLimits := strings.Split(strings.TrimSpace(strings.TrimPrefix(limitUnit, "limited:")), ",")
						for _, individualLimit := range individualLimits {
							item := strings.TrimSpace(individualLimit)
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
					case "ExceededNodeResources":
						// in conjunction with 'kubectl top node', typically see either MEM or CPU over 100% on at least 1 node
						/*
							0s          Warning   ExceededNodeResources    taskrun/465395d14f7f8851e2055721ae43b815-build-0-task                         Insufficient resources to schedule pod "465395d14f7f8851e2055721ae43b815-build-0-task-pod"
							0s          Normal    ExceededNodeResources    taskrun/465395d14f7f8851e2055721ae43b815-build-0-task                         TaskRun Pod exceeded available resources
						*/
						nodeResource++
					}
				}
				/*
					Only pods, not TRs or PRs, have the unschedulable condition
					  status:
					    conditions:
					    - lastProbeTime: null
					      lastTransitionTime: "2023-03-31T00:50:41Z"
					      message: '0/6 nodes are available: 3 Insufficient memory, 3 node(s) had untolerated
					        taint {node-role.kubernetes.io/master: }. preemption: 0/6 nodes are available:
					        3 No preemption victims found for incoming pod, 3 Preemption is not helpful
					        for scheduling.'
					      reason: Unschedulable
					      status: "False"
					      type: PodScheduled
					    phase: Pending

				*/
			}
		}
		limitMemMap[ns] = limitMem
		limitCPUMap[ns] = limitCPU
		reqMemMap[ns] = requestMem
		reqCPUMap[ns] = requestCPU
		podCountMap[ns] = podCount
		nodeResourceMap[ns] = nodeResource
	}
	for ns, limitMem := range limitMemMap {
		ch <- prometheus.MustNewConstMetric(limitMemDesc, prometheus.GaugeValue, float64(limitMem), ns)
	}
	for ns, limitCPU := range limitCPUMap {
		ch <- prometheus.MustNewConstMetric(limitCPUDesc, prometheus.GaugeValue, float64(limitCPU), ns)
	}
	for ns, requestMem := range reqMemMap {
		ch <- prometheus.MustNewConstMetric(requestMemDesc, prometheus.GaugeValue, float64(requestMem), ns)
	}
	for ns, requestCPU := range reqCPUMap {
		ch <- prometheus.MustNewConstMetric(requestCPUDesc, prometheus.GaugeValue, float64(requestCPU), ns)
	}
	for ns, podCount := range podCountMap {
		ch <- prometheus.MustNewConstMetric(podCountDesc, prometheus.GaugeValue, float64(podCount), ns)
	}
	for ns, nodeResource := range nodeResourceMap {
		ch <- prometheus.MustNewConstMetric(nodeResourceDesc, prometheus.GaugeValue, float64(nodeResource), ns)
	}

}
