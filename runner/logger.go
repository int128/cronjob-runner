package runner

import "fmt"

// Logger is an interface to handle the logs.
type Logger interface {
	// PrintContainerLog prints the container log.
	// It is called by every log line.
	PrintContainerLog(rawTimestamp, namespace, podName, containerName, message string)
}

type defaultLogger struct{}

func (defaultLogger) PrintContainerLog(rawTimestamp, namespace, podName, containerName, message string) {
	fmt.Printf("|%s|%s|%s|%s| %s", rawTimestamp, namespace, podName, containerName, message)
}
