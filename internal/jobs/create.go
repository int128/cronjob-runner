package jobs

import (
	"fmt"
	"io"
	"log/slog"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/cli-runtime/pkg/printers"
	"k8s.io/utils/ptr"
)

// NewFromCronJob creates a job from the CronJob template.
// If env is given, it injects the environment variables to all containers.
func NewFromCronJob(cronJob *batchv1.CronJob, env, secretEnv map[string]string, secretRef *corev1.LocalObjectReference) *batchv1.Job {
	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:    cronJob.Namespace,
			GenerateName: fmt.Sprintf("%s-", cronJob.Name),
			// Set the owner reference to clean up the outdated jobs by CronJob controller.
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: batchv1.SchemeGroupVersion.String(),
				Kind:       "CronJob",
				Name:       cronJob.GetName(),
				UID:        cronJob.GetUID(),
				Controller: ptr.To(true),
			}},
			Labels:      cronJob.Spec.JobTemplate.Labels,
			Annotations: cronJob.Spec.JobTemplate.Annotations,
		},
		Spec: appendSecretEnv(appendEnv(cronJob.Spec.JobTemplate.Spec, env), secretEnv, secretRef),
	}
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

func appendSecretEnv(jobSpec batchv1.JobSpec, secretEnv map[string]string, secretRef *corev1.LocalObjectReference) batchv1.JobSpec {
	if secretRef == nil {
		return jobSpec
	}
	if len(secretEnv) == 0 {
		return jobSpec
	}

	var newContainers []corev1.Container
	for _, container := range jobSpec.Template.Spec.Containers {
		var newEnv []corev1.EnvVar
		newEnv = append(newEnv, container.Env...)
		for key := range secretEnv {
			newEnv = append(newEnv, corev1.EnvVar{
				Name: key,
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: *secretRef,
						Key:                  key,
					},
				},
			})
		}
		newContainer := container.DeepCopy()
		newContainer.Env = newEnv
		newContainers = append(newContainers, *newContainer)
	}
	newSpec := jobSpec.DeepCopy()
	newSpec.Template.Spec.Containers = newContainers
	return *newSpec
}

func PrintYAML(job *batchv1.Job, w io.Writer) {
	newJob := job.DeepCopy()
	// YAMLPrinter requires GVK
	newJob.SetGroupVersionKind(batchv1.SchemeGroupVersion.WithKind("Job"))
	// Hide the managed fields
	newJob.SetManagedFields(nil)
	var printer printers.YAMLPrinter
	if err := printer.PrintObj(newJob, w); err != nil {
		slog.Warn("Internal error: printer.PrintObj", "error", err)
	}
}
