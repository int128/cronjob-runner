package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"

	"github.com/spf13/pflag"
	batchv1 "k8s.io/api/batch/v1"
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
	jobConditionType, err := waitForJob(ctx, clientset, job.Namespace, job.Name)
	if err != nil {
		return fmt.Errorf("could not wait for the Job: %w", err)
	}
	if jobConditionType == batchv1.JobFailed {
		return fmt.Errorf("the Job %s/%s was failed")
	}
	return nil
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
