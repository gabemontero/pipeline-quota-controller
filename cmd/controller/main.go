package main

import (
	"flag"
	"os"

	// needed for hack/update-codegen.sh
	_ "k8s.io/code-generator"

	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	//+kubebuilder:scaffold:imports
	"github.com/gabemontero/pipeline-quota-controller/pkg/controller"
	"github.com/go-logr/logr"
)

var (
	mainLog logr.Logger
)

func main() {
	var metricsAddr string
	var enableLeaderElection bool
	var probeAddr string
	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")

	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	klog.InitFlags(flag.CommandLine)
	flag.Parse()

	logger := zap.New(zap.UseFlagOptions(&opts))
	ctrl.SetLogger(logger)
	mainLog = ctrl.Log.WithName("main")
	ctx := ctrl.SetupSignalHandler()
	restConfig := ctrl.GetConfigOrDie()

	var mgr ctrl.Manager
	var err error
	mopts := ctrl.Options{
		MetricsBindAddress:     metricsAddr,
		Port:                   9443,
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "5483be8f.redhat.com",
	}

	mgr, err = controller.NewManager(restConfig, mopts)
	if err != nil {
		mainLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	//+kubebuilder:scaffold:builder

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		mainLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		mainLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	mainLog.Info("starting manager")
	if err := mgr.Start(ctx); err != nil {
		mainLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
