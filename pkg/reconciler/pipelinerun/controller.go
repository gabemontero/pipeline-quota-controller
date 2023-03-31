package pipelinerun

import (
	//"github.com/gabemontero/pipeline-quota-controller/pkg/reconciler/events"
	"github.com/tektoncd/pipeline/pkg/apis/pipeline/v1beta1"

	ctrl "sigs.k8s.io/controller-runtime"
)

//var eventReconciler *events.Controller

func SetupPRReconcilerWithManager(mgr ctrl.Manager) error {
	//var err error
	//eventReconciler, err = events.NewController(mgr.GetConfig())
	//mgr.Add(eventReconciler)
	//if err != nil {
	//	return err
	//}
	//events.InitPrometheus(mgr, eventReconciler)
	InitPrometheus(mgr.GetClient())
	r := newPRReconciler(mgr) //, eventReconciler)
	return ctrl.NewControllerManagedBy(mgr).For(&v1beta1.PipelineRun{}).Complete(r)
}
