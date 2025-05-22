package pods

import (
	"fmt"
	"log/slog"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
)

type Informer interface {
	// Shutdown implements informers.SharedInformerFactory#Shutdown
	Shutdown()
}

// ContainerStartedEvent is sent when a container is started.
type ContainerStartedEvent struct {
	Namespace     string
	PodName       string
	ContainerName string
}

// StartInformer an informer to receive the change of pod resource.
// It finds the corresponding pod(s) by job name.
// You must finally close stopCh to stop the informer.
// When the status of container is changed, the event is sent to containerStartedCh.
func StartInformer(
	clientset kubernetes.Interface,
	namespace, jobName string,
	stopCh <-chan struct{},
	containerStartedCh chan<- ContainerStartedEvent,
) (Informer, error) {
	informerFactory := informers.NewSharedInformerFactoryWithOptions(clientset, time.Hour*24,
		informers.WithNamespace(namespace),
		informers.WithTweakListOptions(func(options *metav1.ListOptions) {
			options.LabelSelector = fmt.Sprintf("batch.kubernetes.io/job-name=%s", jobName)
		}),
	)
	informer := informerFactory.Core().V1().Pods().Informer()
	if _, err := informer.AddEventHandler(&eventHandler{containerStartedCh: containerStartedCh}); err != nil {
		return nil, fmt.Errorf("add an event handler to the informer: %w", err)
	}
	informerFactory.Start(stopCh)
	slog.Info("Watching Pod",
		slog.Group("job", slog.String("namespace", namespace), slog.String("name", jobName)))
	return informerFactory, nil
}

type eventHandler struct {
	containerStartedCh chan<- ContainerStartedEvent
}

func (h *eventHandler) OnAdd(obj interface{}, isInInitialList bool) {
	pod := obj.(*corev1.Pod)
	podAttr := slog.Group("pod", slog.String("namespace", pod.Namespace), slog.String("name", pod.Name), slog.Any("phase", pod.Status.Phase))
	if isInInitialList {
		slog.Info("Pod is found", podAttr)
		return
	}
	slog.Info("Pod is created", podAttr)
}

func (h *eventHandler) OnUpdate(oldObj, newObj interface{}) {
	oldPod := oldObj.(*corev1.Pod)
	newPod := newObj.(*corev1.Pod)
	h.notifyPodStatusChange(oldPod, newPod)
	h.notifyPodConditionScheduled(oldPod, newPod)
	h.notifyPodConditionDisruptionTarget(oldPod, newPod)
	h.notifyContainerStatusChanges(newPod.Namespace, newPod.Name, oldPod.Status.InitContainerStatuses, newPod.Status.InitContainerStatuses)
	h.notifyContainerStatusChanges(newPod.Namespace, newPod.Name, oldPod.Status.ContainerStatuses, newPod.Status.ContainerStatuses)
	h.notifyContainerStarted(newPod.Namespace, newPod.Name, oldPod.Status.InitContainerStatuses, newPod.Status.InitContainerStatuses)
	h.notifyContainerStarted(newPod.Namespace, newPod.Name, oldPod.Status.ContainerStatuses, newPod.Status.ContainerStatuses)
}

func (h *eventHandler) notifyPodStatusChange(oldPod, newPod *corev1.Pod) {
	if oldPod.Status.Phase == newPod.Status.Phase {
		return
	}
	podAttr := slog.Group("pod", slog.String("namespace", newPod.Namespace), slog.String("name", newPod.Name), slog.Any("phase", newPod.Status.Phase))
	switch newPod.Status.Phase {
	case corev1.PodRunning:
		slog.Info("Pod is running", podAttr)
	case corev1.PodSucceeded:
		slog.Info("Pod is succeeded", podAttr)
	case corev1.PodFailed:
		slog.Info("Pod is failed", podAttr,
			slog.String("reason", newPod.Status.Reason),
			slog.String("message", newPod.Status.Message),
		)
	default:
		slog.Info("Pod phase is changed", podAttr,
			slog.String("reason", newPod.Status.Reason),
			slog.String("message", newPod.Status.Message),
		)
	}
}

func (h *eventHandler) notifyPodConditionScheduled(oldPod, newPod *corev1.Pod) {
	podAttr := slog.Group("pod", slog.String("namespace", newPod.Namespace), slog.String("name", newPod.Name))
	condition := findChangedPodConditionByType(corev1.PodScheduled, oldPod.Status.Conditions, newPod.Status.Conditions)
	if condition.Status == corev1.ConditionTrue {
		slog.Info("Pod is scheduled", podAttr, slog.String("node", newPod.Spec.NodeName))
	}
	if condition.Status == corev1.ConditionFalse {
		slog.Info("Pod is not scheduled", podAttr,
			slog.String("reason", condition.Reason),
			slog.String("message", condition.Message))
	}
}

func (h *eventHandler) notifyPodConditionDisruptionTarget(oldPod, newPod *corev1.Pod) {
	podAttr := slog.Group("pod", slog.String("namespace", newPod.Namespace), slog.String("name", newPod.Name), slog.String("node", newPod.Spec.NodeName))
	condition := findChangedPodConditionByType(corev1.DisruptionTarget, oldPod.Status.Conditions, newPod.Status.Conditions)
	if condition.Status == corev1.ConditionTrue {
		slog.Info("Pod will be terminated due to a disruption", podAttr,
			slog.String("reason", condition.Reason),
			slog.String("message", condition.Message))
	}
}

func findChangedPodConditionByType(conditionType corev1.PodConditionType, oldConditions, newConditions []corev1.PodCondition) corev1.PodCondition {
	oldCondition := findPodConditionByType(conditionType, oldConditions)
	newCondition := findPodConditionByType(conditionType, newConditions)
	if oldCondition.Status != newCondition.Status && newCondition.Type != "" {
		return newCondition
	}
	return corev1.PodCondition{}
}

func findPodConditionByType(conditionType corev1.PodConditionType, conditions []corev1.PodCondition) corev1.PodCondition {
	for _, condition := range conditions {
		if condition.Type == conditionType {
			return condition
		}
	}
	return corev1.PodCondition{}
}

func (h *eventHandler) notifyContainerStatusChanges(namespace, podName string, oldStatuses, newStatuses []corev1.ContainerStatus) {
	containerStateChanges := computeContainerStateChanges(oldStatuses, newStatuses)
	for _, change := range containerStateChanges {
		podAttr := slog.Group("pod", slog.String("namespace", namespace), slog.String("name", podName))
		containerAttr := slog.Group("container", slog.String("name", change.newStatus.Name))
		switch change.newState {
		case containerStateWaiting:
			waiting := change.newStatus.State.Waiting
			slog.Info("Container is waiting", podAttr, containerAttr,
				slog.String("reason", waiting.Reason),
				slog.String("message", waiting.Message),
			)
		case containerStateRunning:
			slog.Info("Container is running", podAttr, containerAttr)
		case containerStateTerminated:
			terminated := change.newStatus.State.Terminated
			slog.Info("Container is terminated", podAttr, containerAttr,
				slog.Int("exitCode", int(terminated.ExitCode)),
				slog.String("reason", terminated.Reason),
				slog.String("message", terminated.Message),
			)
		}
	}
}

func (h *eventHandler) notifyContainerStarted(namespace, podName string, oldStatuses, newStatuses []corev1.ContainerStatus) {
	containerStateChanges := computeContainerStateChanges(oldStatuses, newStatuses)
	for _, change := range containerStateChanges {
		oldState := getContainerState(change.oldStatus)
		newState := getContainerState(change.newStatus)
		// Send an event to the channel on the following changes:
		// - Waiting -> Running
		// - Waiting -> Terminated
		// - Terminated -> Running
		if (oldState == containerStateWaiting && newState != containerStateWaiting) ||
			(oldState == containerStateTerminated && newState == containerStateRunning) {
			h.containerStartedCh <- ContainerStartedEvent{
				Namespace:     namespace,
				PodName:       podName,
				ContainerName: change.newStatus.Name,
			}
		}
	}
}

type containerState int

const (
	containerStateWaiting containerState = iota
	containerStateRunning
	containerStateTerminated
)

type containerStateChange struct {
	oldStatus corev1.ContainerStatus
	newStatus corev1.ContainerStatus
	oldState  containerState
	newState  containerState
}

func computeContainerStateChanges(oldStatuses, newStatuses []corev1.ContainerStatus) []containerStateChange {
	var changed []containerStateChange
	oldMap := mapContainerStatusByName(oldStatuses)
	newMap := mapContainerStatusByName(newStatuses)
	for containerName := range newMap {
		oldState := getContainerState(oldMap[containerName])
		newState := getContainerState(newMap[containerName])
		if oldState != newState {
			changed = append(changed, containerStateChange{
				oldStatus: oldMap[containerName],
				newStatus: newMap[containerName],
				oldState:  oldState,
				newState:  newState,
			})
		}
	}
	return changed
}

func mapContainerStatusByName(containerStatuses []corev1.ContainerStatus) map[string]corev1.ContainerStatus {
	containerStatusMap := make(map[string]corev1.ContainerStatus)
	for _, containerStatus := range containerStatuses {
		containerStatusMap[containerStatus.Name] = containerStatus
	}
	return containerStatusMap
}

func getContainerState(containerStatus corev1.ContainerStatus) containerState {
	// According to corev1.ContainerState, either member is set.
	// If none of them is specified, default to corev1.ContainerStateWaiting.
	if containerStatus.State.Waiting != nil {
		return containerStateWaiting
	}
	if containerStatus.State.Running != nil {
		return containerStateRunning
	}
	if containerStatus.State.Terminated != nil {
		return containerStateTerminated
	}
	return containerStateWaiting
}

func (h *eventHandler) OnDelete(obj interface{}) {
	pod := obj.(*corev1.Pod)
	slog.Info("Pod is deleted",
		slog.Group("pod", slog.String("namespace", pod.Namespace), slog.String("name", pod.Name)))
}
