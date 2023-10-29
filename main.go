package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"

	"github.com/int128/cronjob-runner/internal/logs"
	"github.com/spf13/pflag"
	batchv1 "k8s.io/api/batch/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/kubernetes"
)

func run(o options) error {
	ctx := context.Background()
	ctx, stopNotifyCtx := signal.NotifyContext(ctx, os.Interrupt)
	defer stopNotifyCtx()

	restCfg, err := o.k8sFlags.ToRESTConfig()
	if err != nil {
		return fmt.Errorf("could not load the config: %w", err)
	}
	namespace, _, err := o.k8sFlags.ToRawKubeConfigLoader().Namespace()
	if err != nil {
		return fmt.Errorf("could not determine the namespace: %w", err)
	}
	clientset, err := kubernetes.NewForConfig(restCfg)
	if err != nil {
		return fmt.Errorf("could not create a Kubernetes client: %w", err)
	}
	serverVersion, err := clientset.ServerVersion()
	if err != nil {
		return fmt.Errorf("could not get the server version: %w", err)
	}
	log.Printf("Cluster version %s", serverVersion)

	job, err := createJobFromCronJob(ctx, clientset, namespace, o.cronJobName)
	if err != nil {
		return fmt.Errorf("could not create a Job from CronJob: %w", err)
	}

	var backgroundWaiter wait.Group
	defer func() {
		backgroundWaiter.Wait()
		log.Printf("Stopped background workers")
	}()
	jobFinishedCh := make(chan batchv1.JobConditionType)
	defer close(jobFinishedCh)
	stopCh := make(chan struct{})
	defer close(stopCh)

	podInformer, err := startPodInformer(clientset, job.Namespace, job.Name, stopCh,
		func(namespace, podName, containerName string) {
			backgroundWaiter.Start(func() {
				logs.Tail(ctx, clientset, namespace, podName, containerName)
			})
		})
	if err != nil {
		return fmt.Errorf("could not start the pod informer: %w", err)
	}
	backgroundWaiter.Start(podInformer.Shutdown)
	jobInformer, err := startJobInformer(clientset, job.Namespace, job.Name, stopCh, jobFinishedCh)
	if err != nil {
		return fmt.Errorf("could not start the job informer: %w", err)
	}
	backgroundWaiter.Start(jobInformer.Shutdown)
	select {
	case jobConditionType := <-jobFinishedCh:
		if jobConditionType == batchv1.JobFailed {
			return fmt.Errorf("job %s/%s failed", job.Namespace, job.Name)
		}
		return nil
	case <-ctx.Done():
		log.Printf("Shutting down: %s", ctx.Err())
		return ctx.Err()
	}
}

type options struct {
	k8sFlags    *genericclioptions.ConfigFlags
	cronJobName string
}

func (o *options) addFlags(f *pflag.FlagSet) {
	o.k8sFlags.AddFlags(f)
	f.StringVarP(&o.cronJobName, "cron-job-name", "", "", "Name of CronJob")
}

func main() {
	log.SetFlags(log.Lmicroseconds | log.Lshortfile)
	flagSet := pflag.NewFlagSet("cronjob-runner", pflag.ContinueOnError)
	var o options
	o.k8sFlags = genericclioptions.NewConfigFlags(false)
	o.addFlags(flagSet)
	if err := flagSet.Parse(os.Args); err != nil {
		log.Fatalf("Invalid flags: %s", err)
	}
	if err := run(o); err != nil {
		log.Fatalf("Error: %s", err)
	}
}
