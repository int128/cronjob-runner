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

func (h *eventHandler) OnUpdate(_, newObj interface{}) {
	job := newObj.(*batchv1.Job)
	condition := findCondition(job)
	if condition == nil {
		return
	}
	switch condition.Type {
	case batchv1.JobComplete:
		slog.Info("Job is completed",
			slog.Group("job",
				slog.String("namespace", job.Namespace),
				slog.String("name", job.Name),
			),
			slog.Group("condition",
				slog.Any("type", condition.Type),
				slog.String("reason", condition.Reason),
				slog.String("message", condition.Message),
			))
		h.finishedCh <- condition.Type

	case batchv1.JobFailed:
		slog.Info("Job is failed",
			slog.Group("job",
				slog.String("namespace", job.Namespace),
				slog.String("name", job.Name),
			),
			slog.Group("condition",
				slog.String("reason", condition.Reason),
				slog.String("message", condition.Message),
			))
		h.finishedCh <- condition.Type

	default:
		slog.Info("Job condition is changed",
			slog.Group("job",
				slog.String("namespace", job.Namespace),
				slog.String("name", job.Name),
			),
			slog.Group("condition",
				slog.Any("type", condition.Type),
				slog.String("reason", condition.Reason),
				slog.String("message", condition.Message),
			))
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

func (h *eventHandler) OnDelete(obj interface{}) {
	job := obj.(*batchv1.Job)
	slog.Info("Job is deleted",
		slog.Group("job",
			slog.String("namespace", job.Namespace),
			slog.String("name", job.Name)))
}
