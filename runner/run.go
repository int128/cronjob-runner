// Package runner provides an interface to run a Job from a CronJob.
// It runs a new Job as follows:
//
// - Create a Job from the job template of CronJob.
// - Show the statuses of Job, Pod(s) and container(s) when changed.
// - Tail the log streams of all containers.
// - Wait for the Job to be completed or failed.
//
// CAVEAT: This package is designed for the internal use of cronjob-runner command.
// The specification may be changed in the future.
package runner

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/int128/cronjob-runner/internal/jobs"
	"github.com/int128/cronjob-runner/internal/logs"
	"github.com/int128/cronjob-runner/internal/pods"
	batchv1 "k8s.io/api/batch/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
)

// Options represents a set of options for Run.
type Options struct {
	// Namespace is the namespace of the CronJob.
	// Required.
	Namespace string

	// CronJobName is the name of the CronJob.
	// Required.
	CronJobName string

	// Env is a map of environment variables injected to all containers of a Pod.
	// Optional.
	Env map[string]string
}

func (opts Options) validate() error {
	if opts.Namespace == "" {
		return fmt.Errorf("namespace must be set")
	}
	if opts.CronJobName == "" {
		return fmt.Errorf("cronJobName must be set")
	}
	return nil
}

// JobFailedError represents an error that the Job has failed.
type JobFailedError struct {
	JobNamespace string
	JobName      string
}

func (err JobFailedError) Error() string {
	return fmt.Sprintf("job %s/%s failed", err.JobNamespace, err.JobName)
}

// Run runs a new Job from the CronJob template.
// If the job is succeeded, it returns nil.
// If the job is failed, it returns JobFailedError.
// Otherwise, it returns an error.
// If the context is canceled, it stops gracefully.
func Run(ctx context.Context, clientset kubernetes.Interface, opts Options) error {
	if err := opts.validate(); err != nil {
		return fmt.Errorf("invalid options: %w", err)
	}

	job, err := jobs.CreateFromCronJob(ctx, clientset, opts.Namespace, opts.CronJobName, opts.Env)
	if err != nil {
		return fmt.Errorf("could not create a Job from CronJob: %w", err)
	}
	jobs.PrintYAML(*job, os.Stderr)

	var backgroundWaiter wait.Group
	defer func() {
		// This must be run after close(chan) to avoid deadlock
		backgroundWaiter.Wait()
		slog.Info("Stopped background workers")
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
			return JobFailedError{JobNamespace: job.Namespace, JobName: job.Name}
		}
		return nil
	case <-ctx.Done():
		slog.Info("Shutting down")
		return ctx.Err()
	}
}
