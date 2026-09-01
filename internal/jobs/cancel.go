package jobs

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	applybatchv1 "k8s.io/client-go/applyconfigurations/batch/v1"
	"k8s.io/client-go/kubernetes"
)

func Cancel(ctx context.Context, clientset kubernetes.Interface, namespace, name string) error {
	jobApply := applybatchv1.Job(name, namespace).
		WithSpec(applybatchv1.JobSpec().WithActiveDeadlineSeconds(0))

	if _, err := clientset.BatchV1().Jobs(namespace).Apply(ctx, jobApply,
		metav1.ApplyOptions{Force: true, FieldManager: "cronjob-runner"},
	); err != nil {
		return fmt.Errorf("could not apply activeDeadlineSeconds to the Job: %w", err)
	}
	return nil
}
