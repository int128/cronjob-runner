package main

import (
	"context"
	"fmt"
	"log"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/utils/pointer"
)

func createJobFromCronJob(ctx context.Context, clientset *kubernetes.Clientset, namespace, cronJobName string) (*batchv1.Job, error) {
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

type JobInformer interface {
	Shutdown()
}

func startJobInformer(
	clientset *kubernetes.Clientset,
	namespace, jobName string,
	stopCh <-chan struct{},
	finishedCh chan<- batchv1.JobConditionType,
) (JobInformer, error) {
	informerFactory := informers.NewSharedInformerFactoryWithOptions(clientset, time.Hour*24,
		informers.WithNamespace(namespace),
		informers.WithTweakListOptions(func(options *metav1.ListOptions) {
			options.FieldSelector = fmt.Sprintf("metadata.name=%s", jobName)
		}),
	)
	informer := informerFactory.Batch().V1().Jobs().Informer()
	if _, err := informer.AddEventHandler(&jobEventHandler{finishedCh: finishedCh}); err != nil {
		return nil, fmt.Errorf("could not add an event handler to the informer: %w", err)
	}
	informerFactory.Start(stopCh)
	log.Printf("Watching the job %s/%s", namespace, jobName)
	return informerFactory, nil
}

type jobEventHandler struct {
	// sent when the job is completed or failed
	finishedCh chan<- batchv1.JobConditionType
}

func (h *jobEventHandler) OnAdd(obj interface{}, _ bool) {
	job := obj.(*batchv1.Job)
	log.Printf("Job %s/%s is created", job.Namespace, job.Name)
}

func (h *jobEventHandler) OnUpdate(_, newObj interface{}) {
	job := newObj.(*batchv1.Job)
	condition := findJobCondition(job)
	if condition == nil {
		return
	}
	log.Printf("Job %s/%s is %s %s", job.Namespace, job.Name, condition.Type, formatJobConditionMessage(condition))
	if condition.Type == batchv1.JobComplete || condition.Type == batchv1.JobFailed {
		h.finishedCh <- condition.Type
	}
}

func findJobCondition(job *batchv1.Job) *batchv1.JobCondition {
	for _, condition := range job.Status.Conditions {
		if condition.Status == corev1.ConditionTrue {
			return &condition
		}
	}
	return nil
}

func formatJobConditionMessage(condition *batchv1.JobCondition) string {
	if condition.Message == "" && condition.Reason == "" {
		return ""
	}
	if condition.Message == "" {
		return fmt.Sprintf("(%s)", condition.Reason)
	}
	return fmt.Sprintf("(%s: %s)", condition.Reason, condition.Message)
}

func (h *jobEventHandler) OnDelete(obj interface{}) {
	job := obj.(*batchv1.Job)
	log.Printf("Job %s/%s is deleted", job.Namespace, job.Name)
}
