# cronjob-runner [![go](https://github.com/int128/cronjob-runner/actions/workflows/go.yaml/badge.svg)](https://github.com/int128/cronjob-runner/actions/workflows/go.yaml)

This is a commandline tool to run a `Job` from `CronJob` in Kubernetes.
It is designed for running a one-shot job by a job infrastructure such as GitHub Actions or Jenkins.

## Getting Started

To run a Job from the CronJob,

```shell
cronjob-runner --namespace <namespace> --cronjob-name <cronjob-name>
```
