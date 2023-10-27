package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"time"

	"github.com/spf13/pflag"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/utils/pointer"
)

type jobResourceEventHandler struct {
	jobInformerCh chan batchv1.JobConditionType
}

func (h *jobResourceEventHandler) OnAdd(interface{}, bool) {}
func (h *jobResourceEventHandler) OnDelete(interface{})    {}

func (h *jobResourceEventHandler) OnUpdate(_, newObj interface{}) {
	job := newObj.(*batchv1.Job)
	condition := findJobCondition(job)
	log.Printf("Job %s/%s has the pod(s) of active=%d, succeeded=%d, failed=%d",
		job.Namespace,
		job.Name,
		job.Status.Active,
		job.Status.Succeeded,
		job.Status.Failed,
	)
	if condition == nil {
		return
	}
	if condition.Type == batchv1.JobComplete {
		log.Printf("Job %s/%s has been completed: %s %s", job.Namespace, job.Name, condition.Reason, condition.Message)
		h.jobInformerCh <- condition.Type
		return
	}
	if condition.Type == batchv1.JobFailed {
		log.Printf("Job %s/%s has been failed: %s %s", job.Namespace, job.Name, condition.Reason, condition.Message)
		h.jobInformerCh <- condition.Type
		return
	}
}

func findJobCondition(job *batchv1.Job) *batchv1.JobCondition {
	for _, condition := range job.Status.Conditions {
		if condition.Status == corev1.ConditionTrue {
			return &condition
		}
	}
	return nil
}

func waitForJob(ctx context.Context, clientset *kubernetes.Clientset, namespace, jobName string) (bool, error) {
	informerFactory := informers.NewSharedInformerFactoryWithOptions(clientset, time.Hour*24,
		informers.WithNamespace(namespace),
		informers.WithTweakListOptions(func(options *metav1.ListOptions) {
			options.FieldSelector = fmt.Sprintf("metadata.name=%s", jobName)
		}),
	)
	jobInformer := informerFactory.Batch().V1().Jobs()
	jobInformerCh := make(chan batchv1.JobConditionType)
	defer close(jobInformerCh)
	if _, err := jobInformer.Informer().AddEventHandler(&jobResourceEventHandler{jobInformerCh: jobInformerCh}); err != nil {
		return false, fmt.Errorf("could not add an event handler to the Job informer: %w", err)
	}
	stopCh := make(chan struct{})
	defer close(stopCh)
	informerFactory.Start(stopCh)
	if !cache.WaitForCacheSync(stopCh, jobInformer.Informer().HasSynced) {
		return false, fmt.Errorf("error WaitForCacheSync()")
	}
	select {
	case jobConditionType := <-jobInformerCh:
		log.Printf("Shutting down the Job informer")
		return jobConditionType == batchv1.JobComplete, nil
	case <-ctx.Done():
		log.Printf("Shutting down the Job informer: %s", ctx.Err())
		return true, ctx.Err()
	}
}

func run(o options) error {
	ctx := context.Background()
	ctx, stopNotifyCtx := signal.NotifyContext(ctx, os.Interrupt)
	defer stopNotifyCtx()

	restCfg, err := o.k8sFlags.ToRESTConfig()
	if err != nil {
		return fmt.Errorf("could not load the config: %w", err)
	}
	namespace, _, err := o.k8sFlags.ToRawKubeConfigLoader().Namespace()
	if err != nil {
		return fmt.Errorf("could not determine the namespace: %w", err)
	}
	clientset, err := kubernetes.NewForConfig(restCfg)
	if err != nil {
		return fmt.Errorf("could not create a Kubernetes client: %w", err)
	}

	cronJob, err := clientset.BatchV1().CronJobs(namespace).Get(ctx, o.cronJobName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("could not get the CronJob: %w", err)
	}
	log.Printf("Found the CronJob %s/%s", cronJob.Namespace, cronJob.Name)

	jobTemplate := batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:    cronJob.Namespace,
			GenerateName: fmt.Sprintf("%s-", cronJob.Name),
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: batchv1.SchemeGroupVersion.String(),
				Kind:       "CronJob",
				Name:       cronJob.GetName(),
				UID:        cronJob.GetUID(),
				Controller: pointer.Bool(true),
			}},
		},
		Spec: *cronJob.Spec.JobTemplate.Spec.DeepCopy(),
	}
	job, err := clientset.BatchV1().Jobs(namespace).Create(ctx, &jobTemplate, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("could not create a Job: %w", err)
	}
	log.Printf("Created a Job %s/%s", job.Namespace, job.Name)

	success, err := waitForJob(ctx, clientset, job.Namespace, job.Name)
	if err != nil {
		return fmt.Errorf("could not wait for the Job: %w", err)
	}
	if !success {
		return fmt.Errorf("the Job has been failed")
	}
	return nil
}

type options struct {
	k8sFlags    *genericclioptions.ConfigFlags
	cronJobName string
}

func (o *options) addFlags(f *pflag.FlagSet) {
	o.k8sFlags.AddFlags(f)
	f.StringVarP(&o.cronJobName, "cron-job-name", "", "", "Name of CronJob")
}

func main() {
	log.SetFlags(log.Lmicroseconds | log.Lshortfile)
	flagSet := pflag.NewFlagSet("cronjob-runner", pflag.ContinueOnError)
	var o options
	o.k8sFlags = genericclioptions.NewConfigFlags(false)
	o.addFlags(flagSet)
	if err := flagSet.Parse(os.Args); err != nil {
		log.Fatalf("Invalid flags: %s", err)
	}
	if err := run(o); err != nil {
		log.Fatalf("Error: %s", err)
	}
}
