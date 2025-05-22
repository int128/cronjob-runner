package logs

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"
	"unicode"

	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// Record represents a record of container logs.
type Record struct {
	RawTimestamp  string
	Namespace     string
	PodName       string
	ContainerName string

	// Message is the log line.
	// All trailing whitespaces are trimmed.
	Message string
}

type tailLogger interface {
	Handle(record Record)
}

// Tail tails the container log until the following cases:
//   - Reached to EOF
//   - The Pod is not found (already removed from Node)
//   - The context is canceled
func Tail(ctx context.Context, clientset kubernetes.Interface, namespace, podName, containerName string, tlog tailLogger) {
	logger := slog.With(
		slog.Group("pod", slog.String("namespace", namespace), slog.String("name", podName)),
		slog.Group("container", slog.String("name", containerName)),
	)
	logger.Info("Tailing the container log")
	var t tailer
	for {
		err := t.resume(ctx, clientset, namespace, podName, containerName, tlog)
		if err == nil {
			return
		}
		if kerrors.IsNotFound(err) {
			logger.Warn("Pod was deleted before reached to EOF of the container log", "error", err)
			return
		}
		if errors.Is(err, context.Canceled) {
			logger.Warn("Stopped tailing the container log before reached to EOF", "error", err)
			return
		}
		logger.Warn("Retrying to tail the container log", "error", err)
		time.Sleep(100 * time.Millisecond)
	}
}

type tailer struct {
	lastLogTime *metav1.Time
}

func (t *tailer) resume(ctx context.Context, clientset kubernetes.Interface, namespace, podName, containerName string, tlog tailLogger) error {
	stream, err := clientset.CoreV1().Pods(namespace).GetLogs(podName, &corev1.PodLogOptions{
		Container: containerName,
		Follow:    true,
		// Get the timestamp to resume from the last point when the connection is lost.
		Timestamps: true,
		SinceTime:  t.lastLogTime,
	}).Stream(ctx)
	if err != nil {
		return fmt.Errorf("stream error: %w", err)
	}
	defer func() {
		if err := stream.Close(); err != nil {
			slog.Error("Failed to close the stream", "error", err)
		}
	}()

	reader := bufio.NewReader(stream)
	for {
		line, err := reader.ReadString('\n')
		if line != "" {
			rawTimestamp, metaTime, message := parseLine(line)
			tlog.Handle(Record{
				RawTimestamp:  rawTimestamp,
				Namespace:     namespace,
				PodName:       podName,
				ContainerName: containerName,
				Message:       message,
			})
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

// parseLine parses the line and returns the timestamp and message.
// It trims all trailing whitespaces in the line.
// If it cannot parse the timestamp, it returns the whole line.
func parseLine(line string) (string, *metav1.Time, string) {
	trimmedLine := strings.TrimRightFunc(line, unicode.IsSpace)
	s := strings.SplitN(trimmedLine, " ", 2)
	if len(s) != 2 {
		return "", nil, trimmedLine
	}
	rawTimestamp, message := s[0], s[1]
	t, err := time.Parse(time.RFC3339, rawTimestamp)
	if err != nil {
		slog.Debug("Internal error: invalid log timestamp", "error", err, "rawTimestamp", rawTimestamp)
		return "", nil, trimmedLine
	}
	metaTime := metav1.NewTime(t)
	return rawTimestamp, &metaTime, message
}
