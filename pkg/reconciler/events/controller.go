package events

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/tektoncd/pipeline/pkg/apis/pipeline"
	"github.com/tektoncd/pipeline/pkg/client/clientset/versioned/typed/pipeline/v1"
	"github.com/tektoncd/pipeline/pkg/client/clientset/versioned/typed/pipeline/v1beta1"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
)

const (
	maxDuration time.Duration = 1<<63 - 1
)

var (
	controllerLog = ctrl.Log.WithName("eventsController")
)

type eventInformer struct {
	stopCh          chan struct{}
	informerFactory informers.SharedInformerFactory
	informer        cache.SharedIndexInformer
}

type Controller struct {
	kubeClient          kubernetes.Interface
	tektonClientV1      v1.TektonV1Interface
	tektonClientV1Beta1 v1beta1.TektonV1beta1Interface

	eventWorkqueue workqueue.RateLimitingInterface

	eventInformerMap sync.Map
}

func NewController(cfg *rest.Config) (*Controller, error) {
	c := &Controller{
		kubeClient:          kubernetes.NewForConfigOrDie(cfg),
		tektonClientV1:      v1.NewForConfigOrDie(cfg),
		tektonClientV1Beta1: v1beta1.NewForConfigOrDie(cfg),
		eventInformerMap:    sync.Map{},
		eventWorkqueue: workqueue.NewNamedRateLimitingQueue(
			workqueue.DefaultControllerRateLimiter(), "pipeline-quota-controller-event-changes"),
	}
	return c, nil
}

func NewControllerForTest() *Controller {
	return &Controller{
		eventInformerMap: sync.Map{},
	}
}

func (c *Controller) Start(ctx context.Context) error {
	defer c.eventWorkqueue.ShutDown()

	go wait.Until(c.eventEventProcessor, time.Second, ctx.Done())

	//TODO build and run metrics server a la https://github.com/openshift/csi-driver-shared-resource/blob/master/pkg/controller/controller.go#L117-L123

	<-ctx.Done()

	c.eventInformerMap.Range(func(key, value any) bool {
		informerObj := value.(eventInformer)
		close(informerObj.stopCh)
		return true
	})
	return nil
}

func (c *Controller) RegisterEventInformer(namespace string) error {
	val, ok := c.eventInformerMap.LoadOrStore(namespace, eventInformer{})
	if !ok {
		informerObj := val.(eventInformer)
		// look at list options, labels
		informerObj.informerFactory = informers.NewSharedInformerFactoryWithOptions(c.kubeClient, maxDuration, informers.WithNamespace(namespace))
		informerObj.informer = informerObj.informerFactory.Core().V1().Events().Informer()
		informerObj.informer.AddEventHandler(c.eventEventHandler())
		informerObj.stopCh = make(chan struct{})
		informerObj.informerFactory.Start(informerObj.stopCh)
		if !cache.WaitForCacheSync(informerObj.stopCh, informerObj.informer.HasSynced) {
			return fmt.Errorf("failed to wait for Events informer cache for namespace %q to sync", namespace)
		}
		c.eventInformerMap.Store(namespace, informerObj)
	}
	/* TODO
	with informers.WithTweakListOptions() we can provide a function and can update metav1.ListOptions used for the informer
	to furter filter when events we get
	metav1.ListOptions{
		TypeMeta:             metav1.TypeMeta{},
		LabelSelector:        "",
		FieldSelector:        "",
		Watch:                false,
		AllowWatchBookmarks:  false,
		ResourceVersion:      "",
		ResourceVersionMatch: "",
		TimeoutSeconds:       nil,
		Limit:                0,
		Continue:             "",
	}*/
	return nil
}

func (c *Controller) UnregisterEventInformer(namespace string) {
	val, ok := c.eventInformerMap.Load(namespace)
	if ok {
		informerObj := val.(eventInformer)
		close(informerObj.stopCh)
		c.eventInformerMap.Delete(namespace)
	}
}

func (c *Controller) eventEventHandler() cache.ResourceEventHandlerFuncs {
	return cache.ResourceEventHandlerFuncs{
		AddFunc: func(o interface{}) {
			switch v := o.(type) {
			case *corev1.Event:
				c.eventWorkqueue.Add(v)
			default:
				//log unrecognized type
			}
		},
		UpdateFunc: func(o, n interface{}) {
		},
		DeleteFunc: func(o interface{}) {
		},
	}
}

func (c *Controller) eventEventProcessor() {
	for {
		obj, shutdown := c.eventWorkqueue.Get()
		if shutdown {
			return
		}

		func() {
			defer c.eventWorkqueue.Done(obj)
			event, ok := obj.(*corev1.Event)
			if !ok {
				c.eventWorkqueue.Forget(obj)
				return
			}
			if err := c.syncEvent(event); err != nil {
				c.eventWorkqueue.AddRateLimited(obj)
			} else {
				c.eventWorkqueue.Forget(obj)
			}
		}()
	}
}

func (c *Controller) syncEvent(event *corev1.Event) error {
	eventCopy := event.DeepCopy()
	// could not find constant for this
	if eventCopy.Reason == "ExceededResourceQuota" {
		seg1 := strings.Split(eventCopy.Message, "exceeded quota")
		if len(seg1) < 2 {
			return fmt.Errorf("unexpected stage 1 ExceededResourceQuota msg %s", eventCopy.Message)
		}
		seg2 := strings.Split(seg1[1], ",")
		if len(seg2) < 4 {
			return fmt.Errorf("unexpected stage 2 ExceededResourceQuota msg %s", eventCopy.Message)
		}
		//quotaName := strings.TrimSpace(seg2[0])
		//requestedUnit := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(seg2[1]), "requested:"))
		//usedUnit := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(seg2[2]), "used:"))
		limitUnit := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(seg2[3]), "limited:"))

		apiVersion := eventCopy.InvolvedObject.APIVersion
		kind := eventCopy.InvolvedObject.Kind
		name := eventCopy.InvolvedObject.Name
		ns := eventCopy.InvolvedObject.Namespace
		prName := name
		prNs := ns
		//TODO do we want to label tekton objs ?
		if strings.HasPrefix(apiVersion, pipeline.GroupName) && kind != "PipelineRun" {
			controllerLog.Info(fmt.Sprintf("GGM looking for PR for kind %s ns %s name %s", kind, ns, name))
			switch {
			case strings.HasSuffix(apiVersion, "v1beta1"):
				tr, err := c.tektonClientV1Beta1.TaskRuns(ns).Get(context.Background(), name, metav1.GetOptions{})
				if err != nil {
					return err
				}
				for _, owner := range tr.GetOwnerReferences() {
					if owner.Kind == "PipelineRun" {
						controllerLog.Info(fmt.Sprintf("GGM v1beta1 pr name %s", owner.Name))
						prName = owner.Name
						pr, err2 := c.tektonClientV1Beta1.PipelineRuns(ns).Get(context.Background(), prName, metav1.GetOptions{})
						if err2 == nil {
							if pr.HasStarted() {
								controllerLog.Info(fmt.Sprintf("GGMGGM confirmed pr %s throttled again after started", prName))
							}
						} else {
							controllerLog.Info(fmt.Sprintf("GGM pr get %s err %s", prName, err2.Error()))
						}
						break
					}
				}
			case strings.HasSuffix(apiVersion, "v1"):
				tr, err := c.tektonClientV1.TaskRuns(ns).Get(context.Background(), name, metav1.GetOptions{})
				if err != nil {
					return err
				}
				for _, owner := range tr.GetOwnerReferences() {
					if owner.Kind == "PipelineRun" {
						controllerLog.Info(fmt.Sprintf("GGM v1 pr name %s", owner.Name))
						prName = owner.Name
						pr, err2 := c.tektonClientV1.PipelineRuns(ns).Get(context.Background(), prName, metav1.GetOptions{})
						if err2 == nil {
							if pr.HasStarted() {
								controllerLog.Info(fmt.Sprintf("GGMGGM confirmed pr %s throttled again after started", prName))
							}
						} else {
							controllerLog.Info(fmt.Sprintf("GGM pr get %s err %s", prName, err2.Error()))
						}
						break
					}
				}
			}
		}

		label := prometheus.Labels{}
		label[NamespaceLabel] = ns
		cm := &corev1.ConfigMap{}
		var err error
		cm, err = c.kubeClient.CoreV1().ConfigMaps(prNs).Get(context.Background(), prName, metav1.GetOptions{})
		if err != nil && !errors.IsNotFound(err) {
			return err
		}
		notFound := errors.IsNotFound(err)
		if notFound {
			cm.Namespace = prNs
			cm.Name = prName
		}
		if cm.Data == nil {
			cm.Data = map[string]string{}
		}
		// there can be more than one limit reason, i.e. limited: limits.memory=100Gi,requests.memory=32Gi
		individualLimits := strings.Split(strings.TrimSpace(strings.TrimPrefix(limitUnit, "limited:")), ",")
		for _, individualLimit := range individualLimits {
			item := strings.TrimSpace(individualLimit)
			item = strings.ReplaceAll(item, "=", "")
			switch {
			case strings.HasPrefix(item, string(corev1.ResourceLimitsMemory)):
				cm.Data[item] = item
			case strings.HasPrefix(item, string(corev1.ResourceLimitsCPU)):
				cm.Data[item] = item
			case strings.HasPrefix(item, string(corev1.ResourceRequestsMemory)):
				cm.Data[item] = item
			case strings.HasPrefix(item, string(corev1.ResourceRequestsCPU)):
				cm.Data[item] = item
			case strings.HasPrefix(item, string(corev1.ResourcePods)):
				cm.Data[item] = item
			}
		}
		if len(cm.Data) == 0 {
			controllerLog.Info(fmt.Sprintf("GGM emtpy cm map with limitUnit %s len indiv limits %d", limitUnit, len(individualLimits)))
		} else {
			var buf []byte
			buf, err = json.MarshalIndent(cm, "", "    ")
			if err != nil {
				controllerLog.Info(fmt.Sprintf("GGM configmap json err %s", err.Error()))
			} else {
				cmStr := string(buf)
				controllerLog.Info(fmt.Sprintf("GGM configmap json: %s", cmStr))
			}

			if notFound {
				cm, err = c.kubeClient.CoreV1().ConfigMaps(ns).Create(context.Background(), cm, metav1.CreateOptions{})
				return err
			} else {
				cm, err = c.kubeClient.CoreV1().ConfigMaps(ns).Update(context.Background(), cm, metav1.UpdateOptions{})
				return err
			}
		}
		//TODO corresponding piplelineruns/taskruns/pods could have container condition of corev1.PodReasonUnschedulable
		//TODO will have OOMKilled if that is the case, which can also occur if throttled by limit range
	}

	if eventCopy.Reason == "ExceededNodeResources" {
		// in conjunction with 'kubectl top node', typically see either MEM or CPU over 100% on at least 1 node
		/*
			0s          Warning   ExceededNodeResources    taskrun/465395d14f7f8851e2055721ae43b815-build-0-task                         Insufficient resources to schedule pod "465395d14f7f8851e2055721ae43b815-build-0-task-pod"
			0s          Normal    ExceededNodeResources    taskrun/465395d14f7f8851e2055721ae43b815-build-0-task                         TaskRun Pod exceeded available resources
		*/
	}
	return nil
}
