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
	"k8s.io/client-go/tools/cache"
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

func waitForJob(ctx context.Context, clientset *kubernetes.Clientset, namespace, jobName string) (*batchv1.JobCondition, error) {
	informerFactory := informers.NewSharedInformerFactoryWithOptions(clientset, time.Hour*24,
		informers.WithNamespace(namespace),
		informers.WithTweakListOptions(func(options *metav1.ListOptions) {
			options.FieldSelector = fmt.Sprintf("metadata.name=%s", jobName)
		}),
	)
	jobInformer := informerFactory.Batch().V1().Jobs()
	jobConditionCh := make(chan *batchv1.JobCondition)
	defer close(jobConditionCh)
	if _, err := jobInformer.Informer().AddEventHandler(&jobEventHandler{jobConditionCh: jobConditionCh}); err != nil {
		return nil, fmt.Errorf("could not add an event handler to the Job informer: %w", err)
	}
	stopCh := make(chan struct{})
	defer close(stopCh)
	informerFactory.Start(stopCh)
	if !cache.WaitForCacheSync(stopCh, jobInformer.Informer().HasSynced) {
		return nil, fmt.Errorf("error WaitForCacheSync()")
	}
	select {
	case jobCondition := <-jobConditionCh:
		log.Printf("Shutting down the Job informer")
		return jobCondition, nil
	case <-ctx.Done():
		log.Printf("Shutting down the Job informer: %s", ctx.Err())
		return nil, ctx.Err()
	}
}

type jobEventHandler struct {
	jobConditionCh chan *batchv1.JobCondition
}

func (h *jobEventHandler) OnAdd(interface{}, bool) {}
func (h *jobEventHandler) OnDelete(interface{})    {}

func (h *jobEventHandler) OnUpdate(_, newObj interface{}) {
	job := newObj.(*batchv1.Job)
	condition := findJobCondition(job)
	log.Printf("Job %s/%s has the pod(s) of active=%d, succeeded=%d, failed=%d",
		job.Namespace,
		job.Name,
		job.Status.Active,
		job.Status.Succeeded,
		job.Status.Failed,
	)
	if condition == nil {
		return
	}
	h.jobConditionCh <- condition
}

func findJobCondition(job *batchv1.Job) *batchv1.JobCondition {
	for _, condition := range job.Status.Conditions {
		if condition.Status == corev1.ConditionTrue {
			return &condition
		}
	}
	return nil
}
