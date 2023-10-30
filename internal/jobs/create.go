package jobs

import (
	"context"
	"fmt"
	"log"

	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/utils/pointer"
)

func CreateFromCronJob(ctx context.Context, clientset *kubernetes.Clientset, namespace, cronJobName string) (*batchv1.Job, error) {
	cronJob, err := clientset.BatchV1().CronJobs(namespace).Get(ctx, cronJobName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("get error: %w", err)
	}
	log.Printf("Found the CronJob %s/%s", cronJob.Namespace, cronJob.Name)

	jobTemplate := batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:    cronJob.Namespace,
			GenerateName: fmt.Sprintf("%s-", cronJob.Name),
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: batchv1.SchemeGroupVersion.String(),
				Kind:       "CronJob",
				Name:       cronJob.GetName(),
				UID:        cronJob.GetUID(),
				Controller: pointer.Bool(true),
			}},
		},
		Spec: *cronJob.Spec.JobTemplate.Spec.DeepCopy(),
	}
	job, err := clientset.BatchV1().Jobs(namespace).Create(ctx, &jobTemplate, metav1.CreateOptions{})
	if err != nil {
		return nil, fmt.Errorf("create error: %w", err)
	}
	log.Printf("Created a Job %s/%s", job.Namespace, job.Name)
	return job, nil
}
