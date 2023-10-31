# cronjob-runner [![go](https://github.com/int128/cronjob-runner/actions/workflows/go.yaml/badge.svg)](https://github.com/int128/cronjob-runner/actions/workflows/go.yaml)

This is a command to run a `Job` from `CronJob` in Kubernetes.
It is designed for running a one-shot job by a job infrastructure such as GitHub Actions or Jenkins.

## Getting Started

To run a Job from the CronJob,

```shell
cronjob-runner [--namespace your-namespace] --cronjob-name your-cronjob-name
```

Here is an example output of [simple CronJob](e2e_test/simple.yaml).

```console
$ cronjob-runner --cronjob-name simple
04:03:14.740975 main.go:41: Cluster version v1.27.3
04:03:14.744844 create.go:27: Found the CronJob default/simple
04:03:14.751085 create.go:47: Created a Job default/simple-xv2g4
apiVersion: batch/v1
kind: Job
metadata:
  annotations:
    batch.kubernetes.io/job-tracking: ""
  creationTimestamp: "2023-10-31T04:03:14Z"
  generateName: simple-
  generation: 1
  labels:
    batch.kubernetes.io/controller-uid: d36873c5-e4e2-49ae-9f95-ce8c4858974f
    batch.kubernetes.io/job-name: simple-xv2g4
    controller-uid: d36873c5-e4e2-49ae-9f95-ce8c4858974f
    job-name: simple-xv2g4
  name: simple-xv2g4
  namespace: default
  ownerReferences:
  - apiVersion: batch/v1
    controller: true
    kind: CronJob
    name: simple
    uid: 245bc058-c6f4-406b-91e5-f2bb70481d4d
  resourceVersion: "312"
  uid: d36873c5-e4e2-49ae-9f95-ce8c4858974f
spec:
  backoffLimit: 3
  completionMode: NonIndexed
  completions: 1
  parallelism: 1
  selector:
    matchLabels:
      batch.kubernetes.io/controller-uid: d36873c5-e4e2-49ae-9f95-ce8c4858974f
  suspend: false
  template:
    metadata:
      creationTimestamp: null
      labels:
        batch.kubernetes.io/controller-uid: d36873c5-e4e2-49ae-9f95-ce8c4858974f
        batch.kubernetes.io/job-name: simple-xv2g4
        controller-uid: d36873c5-e4e2-49ae-9f95-ce8c4858974f
        job-name: simple-xv2g4
    spec:
      containers:
      - command:
        - bash
        - -c
        - |
          set -eux
          echo "Hello, world!"
          date
          uname -a
          exit 0
        image: debian:stable
        imagePullPolicy: IfNotPresent
        name: example
        resources: {}
        terminationMessagePath: /dev/termination-log
        terminationMessagePolicy: File
      dnsPolicy: ClusterFirst
      restartPolicy: Never
      schedulerName: default-scheduler
      securityContext: {}
      terminationGracePeriodSeconds: 30
status: {}
04:03:14.751671 informer.go:41: Watching a pod of job default/simple-xv2g4
04:03:14.751708 informer.go:36: Watching the job default/simple-xv2g4
04:03:14.756822 informer.go:47: Job default/simple-xv2g4 is created
04:03:27.590782 informer.go:51: Pod default/simple-xv2g4-gtrv4 is Pending
04:03:34.989551 informer.go:68: Pod default/simple-xv2g4-gtrv4 is Running
04:03:34.989577 informer.go:81: Pod default/simple-xv2g4-gtrv4: Container example is running
04:03:34.989589 tail.go:18: Following the container log of default/simple-xv2g4-gtrv4/example
2023-10-31T04:03:34.032032575Z | default/simple-xv2g4-gtrv4/example | + echo 'Hello, world!'
2023-10-31T04:03:34.032057675Z | default/simple-xv2g4-gtrv4/example | + date
2023-10-31T04:03:34.032061375Z | default/simple-xv2g4-gtrv4/example | + uname -a
2023-10-31T04:03:34.032064275Z | default/simple-xv2g4-gtrv4/example | + exit 0
2023-10-31T04:03:34.032045275Z | default/simple-xv2g4-gtrv4/example | Hello, world!
2023-10-31T04:03:34.032071875Z | default/simple-xv2g4-gtrv4/example | Tue Oct 31 04:03:34 UTC 2023
2023-10-31T04:03:34.032075775Z | default/simple-xv2g4-gtrv4/example | Linux simple-xv2g4-gtrv4 6.2.0-1015-azure #15~22.04.1-Ubuntu SMP Fri Oct  6 13:20:44 UTC 2023 x86_64 GNU/Linux
04:03:35.754953 informer.go:85: Pod default/simple-xv2g4-gtrv4: Container example is terminated with exit code 0 (Completed)
04:03:36.840624 informer.go:68: Pod default/simple-xv2g4-gtrv4 is Succeeded
04:03:37.854908 informer.go:56: Job default/simple-xv2g4 is Complete 
04:03:37.855034 main.go:53: Stopped background workers
```

This command create a Job from the job template of CronJob.
It shows the status of Job, Pods and containers.
It follows the log streams of all containers.

### Inject environment variables

To inject environment variables to all containers,

```shell
cronjob-runner [--namespace your-namespace] --cronjob-name your-cronjob-name --env KEY=VALUE
```

For example,

```console
$ cronjob-runner --cronjob-name CreateItem --env ITEM_NAME=example --env ITEM_PRICE=100
```

Do not inject any secret, because anyone can see it by the log or kubectl command.

## Design

### Owner references

This command sets the owner reference of a Job to the parent CronJob.
Here is the relationship of resources.

```mermaid
graph LR
  CronJob --> Job --> Pod
```

When a Job is completed or failed, the old Job will be cleaned up by CronJob controller.
See [Jobs section of the official document](https://kubernetes.io/docs/concepts/workloads/controllers/job/) for details.

### Less runtime injection

For IaC and GitOps principal, every resource should be managed as code.
This command does not allow any modification of the job template, except the environment variables.
