package logs

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// Tail tails the container log until the following cases:
//   - Reached to EOF
//   - The Pod is not found (already removed from Node)
//   - The context is canceled
func Tail(ctx context.Context, clientset *kubernetes.Clientset, namespace, podName, containerName string) {
	log.Printf("Tailing the container log of %s/%s/%s", namespace, podName, containerName)
	var t tailer
	for {
		err := t.resume(ctx, clientset, namespace, podName, containerName)
		if err == nil {
			return
		}
		if kerrors.IsNotFound(err) {
			log.Printf("Pod %s/%s was deleted before reached to EOF of the container log: %s", namespace, podName, err)
			return
		}
		if errors.Is(err, context.Canceled) {
			log.Printf("Stopped tailing the container log of %s/%s/%s before reached to EOF: %s", namespace, podName, containerName, err)
			return
		}
		log.Printf("Retrying to tail the container log of %s/%s/%s: %s", namespace, podName, containerName, err)
		time.Sleep(100 * time.Millisecond)
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
