package logs

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

func Tail(ctx context.Context, clientset *kubernetes.Clientset, namespace, podName, containerName string) {
	log.Printf("Following the container log of %s/%s/%s", namespace, podName, containerName)
	var t tailer
	for {
		if err := t.resume(ctx, clientset, namespace, podName, containerName); err != nil {
			log.Printf("Retrying to get the container log of %s/%s/%s: %s", namespace, podName, containerName, err)
			time.Sleep(100 * time.Millisecond)
			continue
		}
		return
	}
}

type tailer struct {
	lastLogTime *metav1.Time
}

func (t *tailer) resume(ctx context.Context, clientset *kubernetes.Clientset, namespace, podName, containerName string) error {
	stream, err := clientset.CoreV1().Pods(namespace).GetLogs(podName, &corev1.PodLogOptions{
		Container:  containerName,
		Follow:     true,
		Timestamps: true,
		SinceTime:  t.lastLogTime,
	}).Stream(ctx)
	if err != nil {
		return fmt.Errorf("stream error: %w", err)
	}
	defer stream.Close()

	reader := bufio.NewReader(stream)
	for {
		line, err := reader.ReadString('\n')
		if line != "" {
			rawTimestamp, metaTime, message := parseTimestamp(line)
			fmt.Printf("%s | %s/%s/%s | %s", rawTimestamp, namespace, podName, containerName, message)
			t.lastLogTime = metaTime
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read error: %w", err)
		}
	}
}

func parseTimestamp(line string) (string, *metav1.Time, string) {
	s := strings.SplitN(line, " ", 2)
	if len(s) != 2 {
		return "", nil, line
	}
	rawTimestamp, message := s[0], s[1]
	t, err := time.Parse(time.RFC3339, rawTimestamp)
	if err != nil {
		log.Printf("Internal error: invalid log timestamp: %s", err)
		return "", nil, line
	}
	metaTime := metav1.NewTime(t)
	return rawTimestamp, &metaTime, message
}
