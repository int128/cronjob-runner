package main

import (
	"context"
	"fmt"
	"github.com/int128/cronjob-runner/internal/secrets"
	corev1 "k8s.io/api/core/v1"
	"log"
	"os"
	"os/signal"

	"github.com/int128/cronjob-runner/internal/jobs"
	"github.com/int128/cronjob-runner/internal/logs"
	"github.com/int128/cronjob-runner/internal/pods"
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

	secret, err := secrets.Create(ctx, clientset, namespace, o.cronJobName, o.secretEnvsToMap())
	if err != nil {
		return fmt.Errorf("could not create a Secret for Job: %w", err)
	}
	defer func(secretName string) {
		// root ctx may be canceled at this time
		if err := secrets.Delete(context.Background(), clientset, namespace, secretName); err != nil {
			log.Printf("Could not delete the Secret: %s", err)
		}
	}(secret.Name)

	job, err := jobs.CreateFromCronJob(ctx, clientset, namespace, o.cronJobName, jobs.CreateOptions{
		Env:       o.env,
		SecretEnv: o.secretEnv,
		Secret:    corev1.LocalObjectReference{Name: secret.Name},
	})
	if err != nil {
		return fmt.Errorf("could not create a Job from CronJob: %w", err)
	}
	jobs.PrintYAML(*job, os.Stderr)

	secret, err = secrets.ApplyOwnerReference(ctx, clientset, namespace, secret.Name, job)
	if err != nil {
		return fmt.Errorf("could not apply the owner reference to the Secret: %w", err)
	}

	var backgroundWaiter wait.Group
	defer func() {
		// This must be run after close(chan) to avoid deadlock
		backgroundWaiter.Wait()
		log.Printf("Stopped background workers")
	}()

	jobFinishedCh := make(chan batchv1.JobConditionType)
	defer close(jobFinishedCh)
	stopCh := make(chan struct{})
	defer close(stopCh)
	containerStartedCh := make(chan pods.ContainerStartedEvent)
	defer close(containerStartedCh)

	backgroundWaiter.Start(func() {
		// When a container is started, tail the container logs.
		for event := range containerStartedCh {
			event := event
			backgroundWaiter.Start(func() {
				logs.Tail(ctx, clientset, event.Namespace, event.PodName, event.ContainerName)
			})
		}
	})
	podInformer, err := pods.StartInformer(clientset, job.Namespace, job.Name, stopCh, containerStartedCh)
	if err != nil {
		return fmt.Errorf("could not start the pod informer: %w", err)
	}
	backgroundWaiter.Start(podInformer.Shutdown)
	jobInformer, err := jobs.StartInformer(clientset, job.Namespace, job.Name, stopCh, jobFinishedCh)
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
	env         map[string]string
	secretEnv   []string
}

func (o options) secretEnvsToMap() map[string]string {
	secretEnvMap := make(map[string]string)
	for _, env := range o.secretEnv {
		secretEnvMap[env] = os.Getenv(env)
	}
	return secretEnvMap
}

func main() {
	log.SetFlags(log.Lmicroseconds | log.Lshortfile)
	var o options
	pflag.StringVarP(&o.cronJobName, "cronjob-name", "", "", "Name of CronJob")
	pflag.StringToStringVar(&o.env, "env", nil,
		"Environment variables to set into the all containers, in the form of KEY=VALUE")
	pflag.StringArrayVar(&o.secretEnv, "secret-env", nil,
		"Environment variables of secrets to set into the all containers, in the form of KEY")
	o.k8sFlags = genericclioptions.NewConfigFlags(false)
	o.k8sFlags.AddFlags(pflag.CommandLine)
	pflag.Parse()
	if o.cronJobName == "" {
		log.Fatalf("You need to set --cronjob-name")
	}
	if err := run(o); err != nil {
		log.Fatalf("Error: %s", err)
	}
}
