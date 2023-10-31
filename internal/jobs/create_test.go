package jobs

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
)

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
