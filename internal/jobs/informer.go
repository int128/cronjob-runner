package jobs

import (
	"fmt"
	"log"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
)

type Informer interface {
	Shutdown()
}

func StartInformer(
	clientset *kubernetes.Clientset,
	namespace, jobName string,
	stopCh <-chan struct{},
	finishedCh chan<- batchv1.JobConditionType,
) (Informer, error) {
	informerFactory := informers.NewSharedInformerFactoryWithOptions(clientset, time.Hour*24,
		informers.WithNamespace(namespace),
		informers.WithTweakListOptions(func(options *metav1.ListOptions) {
			options.FieldSelector = fmt.Sprintf("metadata.name=%s", jobName)
		}),
	)
	informer := informerFactory.Batch().V1().Jobs().Informer()
	if _, err := informer.AddEventHandler(&eventHandler{finishedCh: finishedCh}); err != nil {
		return nil, fmt.Errorf("could not add an event handler to the informer: %w", err)
	}
	informerFactory.Start(stopCh)
	log.Printf("Watching the job %s/%s", namespace, jobName)
	return informerFactory, nil
}

type eventHandler struct {
	// sent when the job is completed or failed
	finishedCh chan<- batchv1.JobConditionType
}

func (h *eventHandler) OnAdd(obj interface{}, _ bool) {
	job := obj.(*batchv1.Job)
	log.Printf("Job %s/%s is created", job.Namespace, job.Name)
}

func (h *eventHandler) OnUpdate(_, newObj interface{}) {
	job := newObj.(*batchv1.Job)
	condition := findCondition(job)
	if condition == nil {
		return
	}
	log.Printf("Job %s/%s is %s %s", job.Namespace, job.Name, condition.Type, formatConditionMessage(condition))
	if condition.Type == batchv1.JobComplete || condition.Type == batchv1.JobFailed {
		h.finishedCh <- condition.Type
	}
}

func findCondition(job *batchv1.Job) *batchv1.JobCondition {
	for _, condition := range job.Status.Conditions {
		if condition.Status == corev1.ConditionTrue {
			return &condition
		}
	}
	return nil
}

func formatConditionMessage(condition *batchv1.JobCondition) string {
	if condition.Message == "" && condition.Reason == "" {
		return ""
	}
	if condition.Message == "" {
		return fmt.Sprintf("(%s)", condition.Reason)
	}
	return fmt.Sprintf("(%s: %s)", condition.Reason, condition.Message)
}

func (h *eventHandler) OnDelete(obj interface{}) {
	job := obj.(*batchv1.Job)
	log.Printf("Job %s/%s is deleted", job.Namespace, job.Name)
}
