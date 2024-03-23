package runner

import (
	"fmt"

	"github.com/int128/cronjob-runner/internal/logs"
)

// JobFailedError represents an error that the Job has failed.
type JobFailedError struct {
	JobNamespace string
	JobName      string
}

func (err JobFailedError) Error() string {
	return fmt.Sprintf("job %s/%s failed", err.JobNamespace, err.JobName)
}

// ContainerLogRecord represents a record of container logs.
type ContainerLogRecord = logs.Record

// ContainerLogger is an interface to handle the container logs.
type ContainerLogger interface {
	// Handle processes a line of container logs.
	Handle(record ContainerLogRecord)
}

type defaultContainerLogger struct{}

func (defaultContainerLogger) Handle(record ContainerLogRecord) {
	fmt.Printf("|%s|%s|%s|%s| %s",
		record.RawTimestamp,
		record.Namespace,
		record.PodName,
		record.ContainerName,
		record.Message,
	)
}
