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

func StartInformer(
	clientset *kubernetes.Clientset,
	namespace, jobName string,
	stopCh <-chan struct{},
	notifyContainerRunning func(namespace, podName, containerName string),
) (Informer, error) {
	informerFactory := informers.NewSharedInformerFactoryWithOptions(clientset, time.Hour*24,
		informers.WithNamespace(namespace),
		informers.WithTweakListOptions(func(options *metav1.ListOptions) {
			options.LabelSelector = fmt.Sprintf("batch.kubernetes.io/job-name=%s", jobName)
		}),
	)
	informer := informerFactory.Core().V1().Pods().Informer()
	if _, err := informer.AddEventHandler(&eventHandler{notifyContainerRunning: notifyContainerRunning}); err != nil {
		return nil, fmt.Errorf("could not add an event handler to the informer: %w", err)
	}
	informerFactory.Start(stopCh)
	log.Printf("Watching a pod of job %s/%s", namespace, jobName)
	return informerFactory, nil
}

type eventHandler struct {
	notifyContainerRunning func(namespace, podName, containerName string)
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
}

func (h *eventHandler) notifyPodStatusChange(oldPod, newPod *corev1.Pod) {
	if oldPod.Status.Phase == newPod.Status.Phase {
		return
	}
	log.Printf("Pod %s/%s is %s", newPod.Namespace, newPod.Name, newPod.Status.Phase)
}

func (h *eventHandler) notifyContainerStatusChanges(namespace, podName string, oldStatuses, newStatuses []corev1.ContainerStatus) {
	changedContainerStatuses := computeContainerStateChanges(oldStatuses, newStatuses)
	for _, containerStatus := range changedContainerStatuses {
		if containerStatus.State.Waiting != nil {
			waiting := containerStatus.State.Waiting
			log.Printf("Pod %s/%s: Container %s is waiting %s",
				namespace, podName, containerStatus.Name,
				formatContainerStatusMessage(waiting.Reason, waiting.Message))
		}
		if containerStatus.State.Running != nil {
			log.Printf("Pod %s/%s: Container %s is running", namespace, podName, containerStatus.Name)
			h.notifyContainerRunning(namespace, podName, containerStatus.Name)
		}
		if containerStatus.State.Terminated != nil {
			terminated := containerStatus.State.Terminated
			log.Printf("Pod %s/%s: Container %s is terminated with exit code %d %s",
				namespace, podName, containerStatus.Name, terminated.ExitCode,
				formatContainerStatusMessage(terminated.Reason, terminated.Message))
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

func computeContainerStateChanges(oldStatuses, newStatuses []corev1.ContainerStatus) []corev1.ContainerStatus {
	var changed []corev1.ContainerStatus
	oldMap := mapContainerStatusByName(oldStatuses)
	newMap := mapContainerStatusByName(newStatuses)
	for containerName := range newMap {
		oldState := getContainerState(oldMap[containerName])
		newState := getContainerState(newMap[containerName])
		if oldState != newState {
			changed = append(changed, newMap[containerName])
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
	if containerStatus.State.Waiting != nil {
		return "Waiting"
	}
	if containerStatus.State.Running != nil {
		return "Running"
	}
	if containerStatus.State.Terminated != nil {
		return "Terminated"
	}
	return ""
}

func (h *eventHandler) OnDelete(obj interface{}) {
	pod := obj.(*corev1.Pod)
	log.Printf("Pod %s/%s is deleted", pod.Namespace, pod.Name)
}