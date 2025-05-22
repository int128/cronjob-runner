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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	corev1ac "k8s.io/client-go/applyconfigurations/core/v1"
	metav1ac "k8s.io/client-go/applyconfigurations/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/utils/ptr"
)

// RunCronJobOptions represents a set of options for RunJobFromCronJob.
type RunCronJobOptions struct {
	// Env is a map of environment variables injected to all containers of a Pod.
	// Optional.
	Env map[string]string

	// SecretEnv is a map of environment variables injected to all containers of a Pod via an ephemeral Secret.
	// Optional.
	SecretEnv map[string]string

	// ContainerLogger is an implementation of ContainerLogger interface.
	// Default to the defaultContainerLogger.
	ContainerLogger ContainerLogger
}

// RunJobFromCronJob creates a Job from the existing CronJob, and waits for the completion.
//
// It runs a new Job as follows:
//
//   - Create a Secret if RunCronJobOptions.SecretEnv is set.
//   - Create a Job from the CronJob template.
//   - Wait for the Job. See WaitForJob().
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

	if opts.SecretEnv != nil {
		if err := runJobFromCronJobWithSecret(ctx, clientset, cronJob, opts); err != nil {
			return fmt.Errorf("runJobFromCronJobWithSecret: %w", err)
		}
		return nil
	}

	job, err := clientset.BatchV1().Jobs(namespace).Create(ctx,
		jobs.NewFromCronJob(cronJob, opts.Env, nil, nil),
		metav1.CreateOptions{},
	)
	if err != nil {
		return fmt.Errorf("could not create a Job: %w", err)
	}
	slog.Info("Created a Job",
		slog.Group("job", slog.String("namespace", job.Namespace), slog.String("name", job.Name)))
	printJobYAML(job)

	if err := WaitForJob(ctx, clientset, job, WaitForJobOptions{ContainerLogger: opts.ContainerLogger}); err != nil {
		return fmt.Errorf("run the Job: %w", err)
	}
	return nil
}

func runJobFromCronJobWithSecret(ctx context.Context, clientset kubernetes.Interface, cronJob *batchv1.CronJob, opts RunCronJobOptions) error {
	secret, err := clientset.CoreV1().Secrets(cronJob.Namespace).Create(ctx, &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:    cronJob.Namespace,
			GenerateName: fmt.Sprintf("%s-", cronJob.Name),
		},
		Immutable:  ptr.To(true),
		StringData: opts.SecretEnv,
	}, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("create a Secret: %w", err)
	}
	slog.Info("Created a Secret",
		slog.Group("secret", "namespace", secret.Namespace, "name", secret.Name))

	defer func() {
		if err := clientset.CoreV1().Secrets(secret.Namespace).Delete(ctx, secret.Name, metav1.DeleteOptions{}); err != nil {
			slog.Warn("Could not clean up the Secret",
				slog.Group("secret", "namespace", secret.Namespace, "name", secret.Name))
			return
		}
		slog.Info("Cleaned up the Secret",
			slog.Group("secret", "namespace", secret.Namespace, "name", secret.Name))
	}()

	job, err := clientset.BatchV1().Jobs(cronJob.Namespace).Create(ctx,
		jobs.NewFromCronJob(cronJob, opts.Env, opts.SecretEnv, &corev1.LocalObjectReference{Name: secret.Name}),
		metav1.CreateOptions{},
	)
	if err != nil {
		return fmt.Errorf("could not create a Job: %w", err)
	}
	slog.Info("Created a Job",
		slog.Group("job", slog.String("namespace", job.Namespace), slog.String("name", job.Name)))
	printJobYAML(job)

	secret, err = clientset.CoreV1().Secrets(cronJob.Namespace).Apply(ctx,
		corev1ac.Secret(secret.Name, cronJob.Namespace).WithOwnerReferences(
			&metav1ac.OwnerReferenceApplyConfiguration{
				APIVersion: ptr.To(batchv1.SchemeGroupVersion.String()),
				Kind:       ptr.To("Job"),
				Name:       ptr.To(job.Name),
				UID:        &job.UID,
			},
		),
		metav1.ApplyOptions{FieldManager: "cronjob-runner"},
	)
	if err != nil {
		return fmt.Errorf("apply the owner reference to the Secret: %w", err)
	}
	slog.Info("Applied the owner reference to the Secret",
		slog.Group("secret", "namespace", secret.Namespace, "name", secret.Name))

	if err := WaitForJob(ctx, clientset, job, WaitForJobOptions{ContainerLogger: opts.ContainerLogger}); err != nil {
		return fmt.Errorf("run the Job: %w", err)
	}
	return nil
}

// WaitForJobOptions represents a set of options for WaitForJob.
type WaitForJobOptions struct {
	// ContainerLogger is an implementation of ContainerLogger interface.
	// Default to the defaultContainerLogger.
	ContainerLogger ContainerLogger
}

// WaitForJob waits for the completion of the Job.
//
// It waits for the Job as follows:
//
//   - Show the statuses of Job, Pod(s) and container(s) when changed.
//   - Tail the log streams of all containers.
//   - Wait for the Job to be succeeded or failed.
//
// If the job is succeeded, it returns nil.
// If the job is failed, it returns JobFailedError.
// Otherwise, it returns an error.
// If the context is canceled, it stops gracefully.
func WaitForJob(ctx context.Context, clientset kubernetes.Interface, job *batchv1.Job, opts WaitForJobOptions) error {
	if opts.ContainerLogger == nil {
		opts.ContainerLogger = defaultContainerLogger{}
	}

	stopCh := make(chan struct{})
	containerStartedCh := make(chan pods.ContainerStartedEvent)
	jobFinishedCh := make(chan batchv1.JobConditionType)
	var informerWaiter, containerLoggerWaiter wait.Group
	defer func() {
		close(stopCh)
		informerWaiter.Wait()        // depends on close(stopCh)
		close(containerStartedCh)    // depends on informerWaiter
		close(jobFinishedCh)         // depends on informerWaiter
		containerLoggerWaiter.Wait() // depends on close(containerStartedCh)
		slog.Info("Stopped all background workers")
	}()

	containerLoggerWaiter.Start(func() {
		// When a container is started, tail the container logs.
		for containerStartedEvent := range containerStartedCh {
			e := containerStartedEvent
			containerLoggerWaiter.Start(func() {
				logs.Tail(ctx, clientset, e.Namespace, e.PodName, e.ContainerName, opts.ContainerLogger)
			})
		}
	})
	podInformer, err := pods.StartInformer(clientset, job.Namespace, job.Name, stopCh, containerStartedCh)
	if err != nil {
		return fmt.Errorf("could not start the pod informer: %w", err)
	}
	informerWaiter.Start(podInformer.Shutdown)

	jobInformer, err := jobs.StartInformer(clientset, job.Namespace, job.Name, stopCh, jobFinishedCh)
	if err != nil {
		return fmt.Errorf("could not start the job informer: %w", err)
	}
	informerWaiter.Start(jobInformer.Shutdown)

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
