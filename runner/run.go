// Package runner provides an interface to run a Job from a CronJob.
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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
)

// RunCronJobOptions represents a set of options for RunJobFromCronJob.
type RunCronJobOptions struct {
	// Env is a map of environment variables injected to all containers of a Pod.
	// Optional.
	Env map[string]string

	// ContainerLogger is an implementation of ContainerLogger interface.
	// Default to the defaultContainerLogger.
	ContainerLogger ContainerLogger
}

// RunJobFromCronJob creates a Job from the existing CronJob, and waits for the completion.
//
// It runs a new Job as follows:
//
//   - Create a Job from the CronJob template.
//   - Run the Job. See RunJob().
//
// If the job is succeeded, it returns nil.
// If the job is failed, it returns JobFailedError.
// Otherwise, it returns an error.
// If the context is canceled, it stops gracefully.
func RunJobFromCronJob(ctx context.Context, clientset kubernetes.Interface, namespace, cronJobName string, opts RunCronJobOptions) error {
	cronJob, err := clientset.BatchV1().CronJobs(namespace).Get(ctx, cronJobName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("could not get the CronJob: %w", err)
	}
	slog.Info("Found the CronJob", slog.Group("cronJob",
		slog.String("namespace", cronJob.Namespace),
		slog.String("name", cronJob.Name)))

	job := jobs.NewFromCronJob(cronJob, opts.Env)
	if err := RunJob(ctx, clientset, job, RunJobOptions{ContainerLogger: opts.ContainerLogger}); err != nil {
		return fmt.Errorf("could not run the Job: %w", err)
	}
	return nil
}

// RunJobOptions represents a set of options for RunJob.
type RunJobOptions struct {
	// ContainerLogger is an implementation of ContainerLogger interface.
	// Default to the defaultContainerLogger.
	ContainerLogger ContainerLogger
}

// RunJob creates the Job and waits for the completion.
//
// It runs a new Job as follows:
//
//   - Create a Job.
//   - Show the statuses of Job, Pod(s) and container(s) when changed.
//   - Tail the log streams of all containers.
//   - Wait for the Job to be succeeded or failed.
//
// If the job is succeeded, it returns nil.
// If the job is failed, it returns JobFailedError.
// Otherwise, it returns an error.
// If the context is canceled, it stops gracefully.
func RunJob(ctx context.Context, clientset kubernetes.Interface, job *batchv1.Job, opts RunJobOptions) error {
	if opts.ContainerLogger == nil {
		opts.ContainerLogger = defaultContainerLogger{}
	}

	job, err := clientset.BatchV1().Jobs(job.Namespace).Create(ctx, job, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("could not create a Job: %w", err)
	}
	slog.Info("Created a Job", slog.Group("job",
		slog.String("namespace", job.Namespace),
		slog.String("name", job.Name)))
	printJobYAML(job)

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
				logs.Tail(ctx, clientset, event.Namespace, event.PodName, event.ContainerName, opts.ContainerLogger)
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

func printJobYAML(job *batchv1.Job) {
	// Group for GitHub Actions
	// https://docs.github.com/en/actions/using-workflows/workflow-commands-for-github-actions#grouping-log-lines
	_, _ = fmt.Fprintln(os.Stderr, "::group::Job YAML")
	jobs.PrintYAML(job, os.Stderr)
	_, _ = fmt.Fprintln(os.Stderr, "::endgroup::")
}
