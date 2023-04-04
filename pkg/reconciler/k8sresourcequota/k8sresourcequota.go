package k8sresourcequota

import (
	"context"
	"fmt"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var (
	log = ctrl.Log.WithName("resourcequotareconciler")
)

type ReconcilerResourceQuota struct {
	client        client.Client
	scheme        *runtime.Scheme
	eventRecorder record.EventRecorder
}

func newReconciler(mgr ctrl.Manager) reconcile.Reconciler {
	return &ReconcilerResourceQuota{
		client:        mgr.GetClient(),
		scheme:        mgr.GetScheme(),
		eventRecorder: mgr.GetEventRecorderFor("ResourceQuota"),
	}
}

func (r *ReconcilerResourceQuota) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	rq := &corev1.ResourceQuota{}
	err := r.client.Get(ctx, request.NamespacedName, rq)
	if err != nil && !errors.IsNotFound(err) {
		return ctrl.Result{}, err
	}

	for resourceName, resourceQuantity := range rq.Status.Hard {
		/*usedQuantity*/ _, ok := rq.Status.Used[resourceName]
		if !ok {
			log.Info(fmt.Sprintf("no live usage recorded yet for quota policy %q and resource %q",
				request.NamespacedName.String(),
				resourceName))
		}
		//log.Info(fmt.Sprintf("quota %q has capped resource %s at quantity %s is using %s",
		//	request.NamespacedName.String(),
		//	resourceName,
		//	resourceQuantity.String(),
		//	usedQuantity.String()))
		if resourceQuantity.Value() > 0 {
			//used := usedQuantity.Value()
			//limit := resourceQuantity.Value()
			//ratio := float64(usedQuantity.Value() / resourceQuantity.Value())
			//ratioStr := strconv.FormatFloat(ratio, 'E', -1, 64)
			//log.Info(fmt.Sprintf("with used %d hard %d ratio %s", used, limit, ratioStr))
			//TODO expose ratio as a metric ??  don't see any existing k8s metrics of that ilk
		}

	}

	return reconcile.Result{}, nil
}
