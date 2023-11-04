package pods

import (
	"fmt"
	"log"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
)

type Informer interface {
	Shutdown()
}

// ContainerStartedEvent is sent when the container is started.
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
		return nil, fmt.Errorf("could not add an event handler to the informer: %w", err)
	}
	informerFactory.Start(stopCh)
	log.Printf("Watching a pod of job %s/%s", namespace, jobName)
	return informerFactory, nil
}

type eventHandler struct {
	containerStartedCh chan<- ContainerStartedEvent
}

func (h *eventHandler) OnAdd(obj interface{}, _ bool) {
	pod := obj.(*corev1.Pod)
	log.Printf("Pod %s/%s is %s", pod.Namespace, pod.Name, pod.Status.Phase)
}

func (h *eventHandler) OnUpdate(oldObj, newObj interface{}) {
	oldPod := oldObj.(*corev1.Pod)
	newPod := newObj.(*corev1.Pod)
	h.notifyPodStatusChange(oldPod, newPod)
	h.notifyContainerStatusChanges(newPod.Namespace, newPod.Name, oldPod.Status.InitContainerStatuses, newPod.Status.InitContainerStatuses)
	h.notifyContainerStatusChanges(newPod.Namespace, newPod.Name, oldPod.Status.ContainerStatuses, newPod.Status.ContainerStatuses)
	h.notifyContainerStarted(newPod.Namespace, newPod.Name, oldPod.Status.InitContainerStatuses, newPod.Status.InitContainerStatuses)
	h.notifyContainerStarted(newPod.Namespace, newPod.Name, oldPod.Status.ContainerStatuses, newPod.Status.ContainerStatuses)
}

func (h *eventHandler) notifyPodStatusChange(oldPod, newPod *corev1.Pod) {
	if oldPod.Status.Phase == newPod.Status.Phase {
		return
	}
	log.Printf("Pod %s/%s is %s", newPod.Namespace, newPod.Name, newPod.Status.Phase)
}

func (h *eventHandler) notifyContainerStatusChanges(namespace, podName string, oldStatuses, newStatuses []corev1.ContainerStatus) {
	containerStateChanges := computeContainerStateChanges(oldStatuses, newStatuses)
	for _, change := range containerStateChanges {
		if change.newStatus.State.Waiting != nil {
			waiting := change.newStatus.State.Waiting
			log.Printf("Pod %s/%s: Container %s is waiting %s",
				namespace, podName, change.newStatus.Name,
				formatContainerStatusMessage(waiting.Reason, waiting.Message))
		}
		if change.newStatus.State.Running != nil {
			log.Printf("Pod %s/%s: Container %s is running", namespace, podName, change.newStatus.Name)
		}
		if change.newStatus.State.Terminated != nil {
			terminated := change.newStatus.State.Terminated
			log.Printf("Pod %s/%s: Container %s is terminated with exit code %d %s",
				namespace, podName, change.newStatus.Name, terminated.ExitCode,
				formatContainerStatusMessage(terminated.Reason, terminated.Message))
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
		if (oldState == "Waiting" && newState != "Waiting") || (oldState == "Terminated" && newState == "Running") {
			h.containerStartedCh <- ContainerStartedEvent{Namespace: namespace, PodName: podName, ContainerName: change.newStatus.Name}
		}
	}
}

func formatContainerStatusMessage(reason, message string) string {
	if reason == "" && message == "" {
		return ""
	}
	if message == "" {
		return fmt.Sprintf("(%s)", reason)
	}
	return fmt.Sprintf("(%s, %s)", reason, message)
}

type containerStateChange struct {
	oldStatus corev1.ContainerStatus
	newStatus corev1.ContainerStatus
}

func computeContainerStateChanges(oldStatuses, newStatuses []corev1.ContainerStatus) []containerStateChange {
	var changed []containerStateChange
	oldMap := mapContainerStatusByName(oldStatuses)
	newMap := mapContainerStatusByName(newStatuses)
	for containerName := range newMap {
		oldState := getContainerState(oldMap[containerName])
		newState := getContainerState(newMap[containerName])
		if oldState != newState {
			changed = append(changed, containerStateChange{oldStatus: oldMap[containerName], newStatus: newMap[containerName]})
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

func getContainerState(containerStatus corev1.ContainerStatus) string {
	// According to corev1.ContainerState, either member is set.
	// If none of them is specified, default to corev1.ContainerStateWaiting.
	if containerStatus.State.Waiting != nil {
		return "Waiting"
	}
	if containerStatus.State.Running != nil {
		return "Running"
	}
	if containerStatus.State.Terminated != nil {
		return "Terminated"
	}
	return "Waiting"
}

func (h *eventHandler) OnDelete(obj interface{}) {
	pod := obj.(*corev1.Pod)
	log.Printf("Pod %s/%s is deleted", pod.Namespace, pod.Name)
}
