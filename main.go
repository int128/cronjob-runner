package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/int128/cronjob-runner/runner"
	"github.com/spf13/pflag"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/kubernetes"
)

type options struct {
	runner.RunCronJobOptions
	Namespace   string
	CronJobName string
}

func run(clientset kubernetes.Interface, opts options) error {
	ctx := context.Background()
	ctx, stopNotifyCtx := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stopNotifyCtx()

	cronJob, err := clientset.BatchV1().CronJobs(opts.Namespace).Get(ctx, opts.CronJobName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("could not get the CronJob: %w", err)
	}
	slog.Info("Found the CronJob",
		slog.Group("cronJob",
			slog.String("namespace", cronJob.Namespace),
			slog.String("name", cronJob.Name)))

	return runner.RunCronJob(ctx, clientset, cronJob, opts.RunCronJobOptions)
}

func main() {
	log.SetFlags(log.Lmicroseconds | log.Lshortfile)
	var opts options
	pflag.StringVar(&opts.CronJobName, "cronjob-name", "", "Name of CronJob")
	pflag.StringToStringVar(&opts.Env, "env", nil, "Environment variables to set into the all containers")
	kubernetesFlags := genericclioptions.NewConfigFlags(false)
	kubernetesFlags.AddFlags(pflag.CommandLine)
	pflag.Parse()
	if opts.CronJobName == "" {
		log.Fatalf("You need to set --cronjob-name")
	}

	restCfg, err := kubernetesFlags.ToRESTConfig()
	if err != nil {
		log.Fatalf("Could not load the Kubernetes config: %s", err)
	}
	clientset, err := kubernetes.NewForConfig(restCfg)
	if err != nil {
		log.Fatalf("Could not create a Kubernetes client: %s", err)
	}
	opts.Namespace, _, err = kubernetesFlags.ToRawKubeConfigLoader().Namespace()
	if err != nil {
		log.Fatalf("Could not determine the namespace: %s", err)
	}

	if err := run(clientset, opts); err != nil {
		log.Fatalf("Error: %s", err)
	}
}
