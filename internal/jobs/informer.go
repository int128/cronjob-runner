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
	notifyConditionChange(oldJob, newJob)
	notifyFinished(oldJob, newJob, h.finishedCh)
}

func notifyConditionChange(oldJob, newJob *batchv1.Job) {
	changedConditions := findChangedConditionsToTrue(oldJob.Status.Conditions, newJob.Status.Conditions)
	jobAttr := slog.Group("job",
		slog.String("namespace", newJob.Namespace),
		slog.String("name", newJob.Name),
	)
	for conditionType, condition := range changedConditions {
		switch conditionType {
		case batchv1.JobComplete:
			slog.Info("Job is completed", jobAttr)
		case batchv1.JobFailed:
			slog.Info("Job is failed", jobAttr,
				slog.String("reason", condition.Reason),
				slog.String("message", condition.Message))
		default:
			slog.Info("Job condition is changed", jobAttr,
				slog.Any("conditionType", condition.Type),
				slog.String("reason", condition.Reason),
				slog.String("message", condition.Message))
		}
	}
}

func notifyFinished(oldJob, newJob *batchv1.Job, finishedCh chan<- batchv1.JobConditionType) {
	changedConditions := findChangedConditionsToTrue(oldJob.Status.Conditions, newJob.Status.Conditions)
	for conditionType := range changedConditions {
		if conditionType == batchv1.JobComplete || conditionType == batchv1.JobFailed {
			finishedCh <- conditionType
			return
		}
	}
}

func findChangedConditionsToTrue(oldConditions, newConditions []batchv1.JobCondition) map[batchv1.JobConditionType]batchv1.JobCondition {
	changed := make(map[batchv1.JobConditionType]batchv1.JobCondition)
	oldMap := mapConditionByType(oldConditions)
	newMap := mapConditionByType(newConditions)
	for conditionType := range newMap {
		oldCondition := oldMap[conditionType]
		newCondition := newMap[conditionType]
		if oldCondition.Status != newCondition.Status && newCondition.Status == corev1.ConditionTrue {
			changed[conditionType] = newCondition
		}
	}
	return changed
}

func mapConditionByType(conditions []batchv1.JobCondition) map[batchv1.JobConditionType]batchv1.JobCondition {
	m := make(map[batchv1.JobConditionType]batchv1.JobCondition)
	for _, condition := range conditions {
		m[condition.Type] = condition
	}
	return m
}

func (h *eventHandler) OnDelete(obj interface{}) {
	job := obj.(*batchv1.Job)
	slog.Info("Job is deleted",
		slog.Group("job",
			slog.String("namespace", job.Namespace),
			slog.String("name", job.Name)))
}
