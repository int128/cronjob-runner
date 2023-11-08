package main

import (
	"context"
	"log"
	"os"
	"os/signal"

	"github.com/int128/cronjob-runner/runner"
	"github.com/spf13/pflag"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/kubernetes"
)

func run(clientset kubernetes.Interface, opts runner.Options) error {
	ctx := context.Background()
	ctx, stopNotifyCtx := signal.NotifyContext(ctx, os.Interrupt)
	defer stopNotifyCtx()

	return runner.Run(ctx, clientset, opts)
}

func main() {
	log.SetFlags(log.Lmicroseconds | log.Lshortfile)
	var opts runner.Options
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
