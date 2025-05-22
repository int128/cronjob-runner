package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/int128/cronjob-runner/runner"
	"github.com/spf13/pflag"
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

	return runner.RunJobFromCronJob(ctx, clientset, opts.Namespace, opts.CronJobName, opts.RunCronJobOptions)
}

func main() {
	log.SetFlags(log.Lmicroseconds | log.Lshortfile)

	var opts options
	var secretEnvKeys []string
	pflag.StringVar(&opts.CronJobName, "cronjob-name", "", "Name of CronJob")
	pflag.StringToStringVar(&opts.Env, "env", nil,
		"Environment variables to set into the all containers, in the form of KEY=VALUE")
	pflag.StringArrayVar(&secretEnvKeys, "secret-env", nil,
		"Environment variable keys of secrets to set into the all containers")
	kubernetesFlags := genericclioptions.NewConfigFlags(false)
	kubernetesFlags.AddFlags(pflag.CommandLine)
	pflag.Parse()
	if opts.CronJobName == "" {
		log.Fatalf("You need to set --cronjob-name")
	}
	if len(secretEnvKeys) > 0 {
		opts.SecretEnv = make(map[string]string, len(secretEnvKeys))
		for _, key := range secretEnvKeys {
			opts.SecretEnv[key] = os.Getenv(key)
		}
	}

	restCfg, err := kubernetesFlags.ToRESTConfig()
	if err != nil {
		log.Fatalf("Failed to load the Kubernetes config: %s", err)
	}
	clientset, err := kubernetes.NewForConfig(restCfg)
	if err != nil {
		log.Fatalf("Failed to create a Kubernetes client: %s", err)
	}
	opts.Namespace, _, err = kubernetesFlags.ToRawKubeConfigLoader().Namespace()
	if err != nil {
		log.Fatalf("Failed to determine the namespace: %s", err)
	}

	if err := run(clientset, opts); err != nil {
		log.Fatalf("Error: %s", err)
	}
}
