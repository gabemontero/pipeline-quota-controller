package pipelinerun

import (
	"context"
	"time"

	"github.com/tektoncd/pipeline/pkg/apis/pipeline/v1beta1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	contextTimeout = 300 * time.Second
)

var (
	log = ctrl.Log.WithName("pipelinerunreconciler")
)

type ReconcilePipelineRun struct {
	client        client.Client
	scheme        *runtime.Scheme
	eventRecorder record.EventRecorder
	//eventReconciler *events.Controller
}

func newPRReconciler(mgr ctrl.Manager /*, eventReconciler *events.Controller*/) reconcile.Reconciler {
	return &ReconcilePipelineRun{
		client:        mgr.GetClient(),
		scheme:        mgr.GetScheme(),
		eventRecorder: mgr.GetEventRecorderFor("PendingPipelineRun"),
		//eventReconciler: eventReconciler,
	}
}

func (r *ReconcilePipelineRun) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	// Set the ctx to be Background, as the top-level context for incoming requests.
	var cancel context.CancelFunc
	ctx, cancel = context.WithTimeout(ctx, contextTimeout)
	defer cancel()

	pr := &v1beta1.PipelineRun{}
	prerr := r.client.Get(ctx, request.NamespacedName, pr)
	if prerr != nil {
		if !errors.IsNotFound(prerr) {
			return ctrl.Result{}, prerr
		}
	}
	//err := eventReconciler.RegisterEventInformer(pr.Namespace)
	//if err != nil {
	//	return ctrl.Result{}, err
	//}

	//if !pr.HasStarted() {
	//	log.Info(fmt.Sprintf("GGM pr %s:%s not started yet", pr.Namespace, pr.Name))
	//	return reconcile.Result{}, nil
	//}
	//for _, taskRunStatus := range pr.Status.TaskRuns {
	//	// key ^^ is task run name
	//	if taskRunStatus != nil {
	//		sc := taskRunStatus.Status.GetCondition(apis.ConditionSucceeded)
	//		if sc.Status == corev1.ConditionUnknown {
	//			switch sc.Reason {
	//			// no k8s constants for this; there are tekton ones in pkg/pod but that brings in internal packages that monkey with go mod
	//			case "ExceededResourceQuota":
	//				/*
	//					message: 'TaskRun Pod exceeded available resources: pods "fc4a2449870a0f7119c98ebb771942fe-build-0-task-pod"
	//					              is forbidden: exceeded quota: compute-build, requested: requests.memory=3Gi,
	//					              used: requests.memory=63Gi, limited: requests.memory=64Gi'
	//				*/
	//			case "ExceededNodeResources":
	//				// in conjunction with 'kubectl top node', typically see either MEM or CPU over 100% on at least 1 node
	//				/*
	//					0s          Warning   ExceededNodeResources    taskrun/465395d14f7f8851e2055721ae43b815-build-0-task                         Insufficient resources to schedule pod "465395d14f7f8851e2055721ae43b815-build-0-task-pod"
	//					0s          Normal    ExceededNodeResources    taskrun/465395d14f7f8851e2055721ae43b815-build-0-task                         TaskRun Pod exceeded available resources
	//				*/
	//			}
	//		}
	//	}
	//}
	//
	//if pr.IsDone() || pr.IsCancelled() {
	//	log.Info(fmt.Sprintf("GGM pr %s:%s done %v cancelled %v", pr.Namespace, pr.Name, pr.IsDone(), pr.IsCancelled()))
	//	return reconcile.Result{}, nil
	//}

	return reconcile.Result{}, nil

}
