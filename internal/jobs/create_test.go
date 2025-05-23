package jobs

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	"github.com/google/go-cmp/cmp"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
)

func TestNewFromCronJob(t *testing.T) {
	t.Run("as-is", func(t *testing.T) {
		cronJob := &batchv1.CronJob{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      "example-cronjob",
			},
			Spec: batchv1.CronJobSpec{
				Suspend:  ptr.To(true),
				Schedule: "@annual",
				JobTemplate: batchv1.JobTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels:      map[string]string{"my/label": "foo"},
						Annotations: map[string]string{"my/annotation": "bar"},
					},
					Spec: batchv1.JobSpec{
						BackoffLimit: ptr.To[int32](1),
						Template: corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{{
									Name: "example-container",
								}},
							},
						},
					},
				},
			},
		}
		gotJob := NewFromCronJob(cronJob, nil, nil, nil)
		wantJob := &batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{
				Namespace:    "default",
				GenerateName: "example-cronjob-",
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion: "batch/v1",
					Kind:       "CronJob",
					Name:       "example-cronjob",
					Controller: ptr.To(true),
				}},
				Labels:      map[string]string{"my/label": "foo"},
				Annotations: map[string]string{"my/annotation": "bar"},
			},
			Spec: batchv1.JobSpec{
				BackoffLimit: ptr.To[int32](1),
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{{
							Name: "example-container",
						}},
					},
				},
			},
		}
		if diff := cmp.Diff(wantJob, gotJob); diff != "" {
			t.Errorf("job mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("env is given", func(t *testing.T) {
		cronJob := &batchv1.CronJob{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      "example-cronjob",
			},
			Spec: batchv1.CronJobSpec{
				Suspend:  ptr.To(true),
				Schedule: "@annual",
				JobTemplate: batchv1.JobTemplateSpec{
					Spec: batchv1.JobSpec{
						BackoffLimit: ptr.To[int32](1),
						Template: corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{{
									Name: "example-container",
								}},
							},
						},
					},
				},
			},
		}
		gotJob := NewFromCronJob(cronJob, map[string]string{"FOO": "bar"}, nil, nil)
		wantJob := &batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{
				Namespace:    "default",
				GenerateName: "example-cronjob-",
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion: "batch/v1",
					Kind:       "CronJob",
					Name:       "example-cronjob",
					Controller: ptr.To(true),
				}},
			},
			Spec: batchv1.JobSpec{
				BackoffLimit: ptr.To[int32](1),
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{{
							Name: "example-container",
							Env: []corev1.EnvVar{{
								Name:  "FOO",
								Value: "bar",
							}},
						}},
					},
				},
			},
		}
		if diff := cmp.Diff(wantJob, gotJob); diff != "" {
			t.Errorf("job mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("secretEnv is given", func(t *testing.T) {
		cronJob := &batchv1.CronJob{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      "example-cronjob",
			},
			Spec: batchv1.CronJobSpec{
				Suspend:  ptr.To(true),
				Schedule: "@annual",
				JobTemplate: batchv1.JobTemplateSpec{
					Spec: batchv1.JobSpec{
						BackoffLimit: ptr.To[int32](1),
						Template: corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{{
									Name: "example-container",
								}},
							},
						},
					},
				},
			},
		}
		gotJob := NewFromCronJob(cronJob, nil, map[string]string{"FOO": "bar"}, &corev1.LocalObjectReference{Name: "example-secret"})
		wantJob := &batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{
				Namespace:    "default",
				GenerateName: "example-cronjob-",
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion: "batch/v1",
					Kind:       "CronJob",
					Name:       "example-cronjob",
					Controller: ptr.To(true),
				}},
			},
			Spec: batchv1.JobSpec{
				BackoffLimit: ptr.To[int32](1),
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{{
							Name: "example-container",
							Env: []corev1.EnvVar{{
								Name: "FOO",
								ValueFrom: &corev1.EnvVarSource{
									SecretKeyRef: &corev1.SecretKeySelector{
										LocalObjectReference: corev1.LocalObjectReference{Name: "example-secret"},
										Key:                  "FOO",
									},
								},
							}},
						}},
					},
				},
			},
		}
		if diff := cmp.Diff(wantJob, gotJob); diff != "" {
			t.Errorf("job mismatch (-want +got):\n%s", diff)
		}
	})
}

func Test_appendEnv(t *testing.T) {
	jobSpecWithEnv := batchv1.JobSpec{
		Template: corev1.PodTemplateSpec{
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name: "container1",
						Env: []corev1.EnvVar{
							{Name: "FOO", Value: "bar"},
						},
					},
					{
						Name: "container2",
					},
				},
			},
		},
	}

	t.Run("do nothing if nil is given", func(t *testing.T) {
		got := appendEnv(jobSpecWithEnv, nil)
		want := jobSpecWithEnv
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("appendEnv() mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("map is appended to env of all containers", func(t *testing.T) {
		got := appendEnv(jobSpecWithEnv, map[string]string{"BAZ": "qux"})
		want := batchv1.JobSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "container1",
							Env: []corev1.EnvVar{
								{Name: "FOO", Value: "bar"},
								{Name: "BAZ", Value: "qux"},
							},
						},
						{
							Name: "container2",
							Env: []corev1.EnvVar{
								{Name: "BAZ", Value: "qux"},
							},
						},
					},
				},
			},
		}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("appendEnv() mismatch (-want +got):\n%s", diff)
		}
	})
}

func Test_appendSecretEnv(t *testing.T) {
	jobSpecWithEnv := batchv1.JobSpec{
		Template: corev1.PodTemplateSpec{
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name: "container1",
						Env: []corev1.EnvVar{
							{Name: "FOO", Value: "bar"},
						},
					},
					{
						Name: "container2",
					},
				},
			},
		},
	}

	t.Run("do nothing if nil is given", func(t *testing.T) {
		got := appendSecretEnv(jobSpecWithEnv, nil, nil)
		want := jobSpecWithEnv
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("appendSecretEnv() mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("do nothing if empty map is given", func(t *testing.T) {
		got := appendSecretEnv(jobSpecWithEnv, map[string]string{}, nil)
		want := jobSpecWithEnv
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("appendSecretEnv() mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("do nothing if secretRef is nil", func(t *testing.T) {
		got := appendSecretEnv(jobSpecWithEnv, map[string]string{"BAZ": "qux"}, nil)
		want := jobSpecWithEnv
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("appendSecretEnv() mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("map is appended to env of all containers", func(t *testing.T) {
		got := appendSecretEnv(jobSpecWithEnv, map[string]string{"BAZ": "qux"}, &corev1.LocalObjectReference{Name: "example-secret"})
		want := batchv1.JobSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "container1",
							Env: []corev1.EnvVar{
								{Name: "FOO", Value: "bar"},
								{
									Name: "BAZ",
									ValueFrom: &corev1.EnvVarSource{
										SecretKeyRef: &corev1.SecretKeySelector{
											LocalObjectReference: corev1.LocalObjectReference{Name: "example-secret"},
											Key:                  "BAZ",
										},
									},
								},
							},
						},
						{
							Name: "container2",
							Env: []corev1.EnvVar{
								{
									Name: "BAZ",
									ValueFrom: &corev1.EnvVarSource{
										SecretKeyRef: &corev1.SecretKeySelector{
											LocalObjectReference: corev1.LocalObjectReference{Name: "example-secret"},
											Key:                  "BAZ",
										},
									},
								},
							},
						},
					},
				},
			},
		}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("appendSecretEnv() mismatch (-want +got):\n%s", diff)
		}
	})
}
