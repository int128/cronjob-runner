package jobs

import (
	"fmt"
	"log/slog"
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

// StartInformer starts an informer to receive the change of job resource.
// You must finally close stopCh to stop the informer.
// When the job is completed or failed, it is sent to finishedCh.
func StartInformer(
	clientset kubernetes.Interface,
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
	slog.Info("Watching Job",
		slog.Group("job",
			slog.String("namespace", namespace),
			slog.String("name", jobName)))
	return informerFactory, nil
}

type eventHandler struct {
	finishedCh chan<- batchv1.JobConditionType
}

func (h *eventHandler) OnAdd(obj interface{}, isInInitialList bool) {
	job := obj.(*batchv1.Job)
	if isInInitialList {
		slog.Info("Job is found",
			slog.Group("job",
				slog.String("namespace", job.Namespace),
				slog.String("name", job.Name)))
		return
	}
	slog.Info("Job is created",
		slog.Group("job",
			slog.String("namespace", job.Namespace),
			slog.String("name", job.Name)))
}

func (h *eventHandler) OnUpdate(oldObj, newObj interface{}) {
	oldJob := oldObj.(*batchv1.Job)
	newJob := newObj.(*batchv1.Job)
	notifyConditionChange(oldJob, newJob, h.finishedCh)
}

func notifyConditionChange(oldJob, newJob *batchv1.Job, finishedCh chan<- batchv1.JobConditionType) {
	condition := findChangedCondition(oldJob.Status.Conditions, newJob.Status.Conditions)
	if condition == nil {
		return
	}
	jobAttr := slog.Group("job",
		slog.String("namespace", newJob.Namespace),
		slog.String("name", newJob.Name),
	)
	switch condition.Type {
	case batchv1.JobComplete:
		slog.Info("Job is completed", jobAttr)
		finishedCh <- condition.Type
	case batchv1.JobFailed:
		slog.Info("Job is failed", jobAttr,
			slog.String("reason", condition.Reason),
			slog.String("message", condition.Message))
		finishedCh <- condition.Type
	default:
		slog.Info("Job condition is changed", jobAttr,
			slog.Any("conditionType", condition.Type),
			slog.String("reason", condition.Reason),
			slog.String("message", condition.Message))
	}
}

func findChangedCondition(oldConditions, newConditions []batchv1.JobCondition) *batchv1.JobCondition {
	oldCondition := findTrueCondition(oldConditions)
	newCondition := findTrueCondition(newConditions)
	if newCondition == nil {
		return nil // no condition is available
	}
	if oldCondition != nil || oldCondition.Type == newCondition.Type {
		return nil // condition is not changed
	}
	return newCondition
}

func findTrueCondition(conditions []batchv1.JobCondition) *batchv1.JobCondition {
	for _, condition := range conditions {
		if condition.Status == corev1.ConditionTrue {
			return &condition
		}
	}
	return nil
}

func (h *eventHandler) OnDelete(obj interface{}) {
	job := obj.(*batchv1.Job)
	slog.Info("Job is deleted",
		slog.Group("job",
			slog.String("namespace", job.Namespace),
			slog.String("name", job.Name)))
}
