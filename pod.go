package main

import (
	"fmt"
	"log"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
)

type PodInformer interface {
	Shutdown()
}

func startPodInformer(
	clientset *kubernetes.Clientset,
	namespace, jobName string,
	stopCh <-chan struct{},
) (PodInformer, error) {
	informerFactory := informers.NewSharedInformerFactoryWithOptions(clientset, time.Hour*24,
		informers.WithNamespace(namespace),
		informers.WithTweakListOptions(func(options *metav1.ListOptions) {
			options.LabelSelector = fmt.Sprintf("batch.kubernetes.io/job-name=%s", jobName)
		}),
	)
	informer := informerFactory.Core().V1().Pods().Informer()
	if _, err := informer.AddEventHandler(&podEventHandler{}); err != nil {
		return nil, fmt.Errorf("could not add an event handler to the informer: %w", err)
	}
	informerFactory.Start(stopCh)
	log.Printf("Watching a pod of job %s/%s", namespace, jobName)
	return informerFactory, nil
}

type podEventHandler struct{}

func (h *podEventHandler) OnAdd(obj interface{}, _ bool) {
	pod := obj.(*corev1.Pod)
	log.Printf("Pod %s/%s is %s", pod.Namespace, pod.Name, pod.Status.Phase)
}

func (h *podEventHandler) OnUpdate(oldObj, newObj interface{}) {
	oldPod := oldObj.(*corev1.Pod)
	newPod := newObj.(*corev1.Pod)
	if oldPod.Status.Phase == newPod.Status.Phase {
		return
	}
	log.Printf("Pod %s/%s is %s", newPod.Namespace, newPod.Name, newPod.Status.Phase)

	for _, containerStatus := range newPod.Status.ContainerStatuses {
		if containerStatus.State.Waiting != nil {
			waiting := containerStatus.State.Waiting
			log.Printf("Pod %s/%s: Container %s is waiting %s",
				newPod.Namespace, newPod.Name, containerStatus.Name,
				formatContainerStatusMessage(waiting.Reason, waiting.Message))
		}
		if containerStatus.State.Running != nil {
			log.Printf("Pod %s/%s: Container %s is running",
				newPod.Namespace, newPod.Name, containerStatus.Name)
		}
		if containerStatus.State.Terminated != nil {
			terminated := containerStatus.State.Terminated
			log.Printf("Pod %s/%s: Container %s is terminated with exit code %d %s",
				newPod.Namespace, newPod.Name, containerStatus.Name, terminated.ExitCode,
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

func (h *podEventHandler) OnDelete(obj interface{}) {
	pod := obj.(*corev1.Pod)
	log.Printf("Pod %s/%s is deleted", pod.Namespace, pod.Name)
}
