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
	log.Printf("Tailing the container log of %s/%s/%s", namespace, podName, containerName)
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
	lastTimestamp *metav1.Time
}

func (t *tailer) resume(ctx context.Context, clientset *kubernetes.Clientset, namespace, podName, containerName string) error {
	stream, err := clientset.CoreV1().Pods(namespace).GetLogs(podName, &corev1.PodLogOptions{
		Container:  containerName,
		Follow:     true,
		Timestamps: true,
		SinceTime:  t.lastTimestamp,
	}).Stream(ctx)
	if err != nil {
		return fmt.Errorf("stream error: %w", err)
	}
	defer stream.Close()

	reader := bufio.NewReader(stream)
	for {
		line, err := reader.ReadString('\n')
		if line != "" {
			timestamp, message := parseTimestamp(line)
			fmt.Printf("%s | %s/%s/%s | %s",
				timestamp.Format(time.RFC3339), namespace, podName, containerName, message)
			t.lastTimestamp = timestamp
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read error: %w", err)
		}
	}
}

func parseTimestamp(line string) (*metav1.Time, string) {
	s := strings.SplitN(line, " ", 2)
	if len(s) != 2 {
		return nil, line
	}
	timestamp, message := s[0], s[1]
	t, err := time.Parse(time.RFC3339, timestamp)
	if err != nil {
		log.Printf("Internal error: invalid log timestamp: %s", err)
		return nil, line
	}
	mt := metav1.NewTime(t)
	return &mt, message
}
