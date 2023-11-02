package jobs

import (
	"context"
	"fmt"
	"io"
	"log"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/cli-runtime/pkg/printers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/utils/pointer"
)

// CreateFromCronJob creates a job from the CronJob template.
// If env is given, it injects the environment variables to all containers.
func CreateFromCronJob(
	ctx context.Context,
	clientset *kubernetes.Clientset,
	namespace, cronJobName string,
	env map[string]string,
) (*batchv1.Job, error) {
	cronJob, err := clientset.BatchV1().CronJobs(namespace).Get(ctx, cronJobName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("get error: %w", err)
	}
	log.Printf("Found the CronJob %s/%s", cronJob.Namespace, cronJob.Name)

	jobTemplate := batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:    cronJob.Namespace,
			GenerateName: fmt.Sprintf("%s-", cronJob.Name),
			// Set the owner reference to clean up the outdated jobs by CronJob controller.
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: batchv1.SchemeGroupVersion.String(),
				Kind:       "CronJob",
				Name:       cronJob.GetName(),
				UID:        cronJob.GetUID(),
				Controller: pointer.Bool(true),
			}},
		},
		Spec: appendEnv(cronJob.Spec.JobTemplate.Spec, env),
	}
	job, err := clientset.BatchV1().Jobs(namespace).Create(ctx, &jobTemplate, metav1.CreateOptions{})
	if err != nil {
		return nil, fmt.Errorf("create error: %w", err)
	}
	log.Printf("Created a Job %s/%s", job.Namespace, job.Name)
	return job, nil
}

func appendEnv(jobSpec batchv1.JobSpec, env map[string]string) batchv1.JobSpec {
	if len(env) == 0 {
		return jobSpec
	}

	var newContainers []corev1.Container
	for _, container := range jobSpec.Template.Spec.Containers {
		var newEnv []corev1.EnvVar
		newEnv = append(newEnv, container.Env...)
		for name, value := range env {
			newEnv = append(newEnv, corev1.EnvVar{Name: name, Value: value})
		}
		newContainer := container.DeepCopy()
		newContainer.Env = newEnv
		newContainers = append(newContainers, *newContainer)
	}
	newSpec := jobSpec.DeepCopy()
	newSpec.Template.Spec.Containers = newContainers
	return *newSpec
}

func PrintYAML(job batchv1.Job, w io.Writer) {
	newJob := job.DeepCopy()
	// YAMLPrinter requires GVK
	newJob.SetGroupVersionKind(batchv1.SchemeGroupVersion.WithKind("Job"))
	// Hide the managed fields
	newJob.ObjectMeta.SetManagedFields(nil)
	var printer printers.YAMLPrinter
	if err := printer.PrintObj(newJob, w); err != nil {
		log.Printf("Internal error: printer.PrintObj: %s", err)
	}
}
