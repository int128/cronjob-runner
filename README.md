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
08:57:53.968787 main.go:41: Cluster version v1.27.3
08:57:53.973826 create.go:19: Found the CronJob default/simple
08:57:53.982652 create.go:39: Created a Job default/simple-xjfqx
08:57:53.982737 informer.go:41: Watching a pod of job default/simple-xjfqx
08:57:53.982779 informer.go:36: Watching the job default/simple-xjfqx
08:57:53.985172 informer.go:47: Job default/simple-xjfqx is created
08:58:02.064218 informer.go:51: Pod default/simple-xjfqx-9r8h9 is Pending
08:58:11.340887 informer.go:68: Pod default/simple-xjfqx-9r8h9 is Running
08:58:11.340917 informer.go:81: Pod default/simple-xjfqx-9r8h9: Container example is running
08:58:11.340939 tail.go:18: Following the container log of default/simple-xjfqx-9r8h9/example
2023-10-30T08:58:10.326285646Z | default/simple-xjfqx-9r8h9/example | + echo 'Hello, world!'
2023-10-30T08:58:10.326625577Z | default/simple-xjfqx-9r8h9/example | Hello, world!
2023-10-30T08:58:10.326653080Z | default/simple-xjfqx-9r8h9/example | + date
2023-10-30T08:58:10.327787483Z | default/simple-xjfqx-9r8h9/example | Mon Oct 30 08:58:10 UTC 2023
2023-10-30T08:58:10.330815959Z | default/simple-xjfqx-9r8h9/example | + uname -a
2023-10-30T08:58:10.331616131Z | default/simple-xjfqx-9r8h9/example | Linux simple-xjfqx-9r8h9 6.2.0-1015-azure #15~22.04.1-Ubuntu SMP Fri Oct  6 13:20:44 UTC 2023 x86_64 GNU/Linux
2023-10-30T08:58:10.331814249Z | default/simple-xjfqx-9r8h9/example | + exit 0
08:58:12.090242 informer.go:85: Pod default/simple-xjfqx-9r8h9: Container example is terminated with exit code 0 (Completed)
08:58:13.209885 informer.go:68: Pod default/simple-xjfqx-9r8h9 is Succeeded
08:58:14.231420 informer.go:56: Job default/simple-xjfqx is Complete 
08:58:14.232074 main.go:52: Stopped background workers
```

This command create a Job from the job template of CronJob.
It shows the status of Job, Pods and containers.
It follows the log streams of all containers.

### Inject environment variables

To inject environment variables to all containers,

```shell
cronjob-runner [--namespace your-namespace] --cronjob-name your-cronjob-name --env KEY=VALUE
```

Do not inject any secret, because anyone can see it by the log or kubectl command.

## Design

For IaC and GitOps principal, every resource should be managed as code.
This command does not allow any modification of the job template, except the environment variables.
