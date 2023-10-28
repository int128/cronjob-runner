package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"

	"github.com/spf13/pflag"
	"golang.org/x/sync/errgroup"
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

	job, err := createJobFromCronJob(ctx, clientset, namespace, o.cronJobName)
	if err != nil {
		return fmt.Errorf("could not create a Job from CronJob: %w", err)
	}

	var eg errgroup.Group
	stopCh := make(chan struct{})
	eg.Go(func() error {
		if err := watchJobPod(clientset, job.Namespace, job.Name, stopCh); err != nil {
			log.Printf("Error while watching the pod: %s", err)
		}
		return nil
	})
	eg.Go(func() error {
		defer close(stopCh)
		success, err := waitForJob(ctx, clientset, job.Namespace, job.Name)
		if err != nil {
			return fmt.Errorf("could not wait for the Job: %w", err)
		}
		if !success {
			return fmt.Errorf("job has been failed")
		}
		return nil
	})
	return eg.Wait()
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
