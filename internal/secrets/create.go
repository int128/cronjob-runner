package secrets

import (
	"context"
	"fmt"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	corev1ac "k8s.io/client-go/applyconfigurations/core/v1"
	metav1ac "k8s.io/client-go/applyconfigurations/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/utils/pointer"
	"log"
)

func Create(
	ctx context.Context,
	clientset kubernetes.Interface,
	namespace, cronJobName string,
	secretEnvMap map[string]string,
) (*corev1.Secret, error) {
	secretToCreate := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:    namespace,
			GenerateName: fmt.Sprintf("%s-", cronJobName),
		},
		Immutable:  pointer.Bool(true),
		StringData: secretEnvMap,
	}
	secret, err := clientset.CoreV1().Secrets(namespace).Create(ctx, &secretToCreate, metav1.CreateOptions{})
	if err != nil {
		return nil, fmt.Errorf("create error: %w", err)
	}
	log.Printf("Created a Secret %s/%s", secret.Namespace, secret.Name)
	return secret, nil
}

func ApplyOwnerReference(
	ctx context.Context,
	clientset kubernetes.Interface,
	namespace, secretName string,
	job *batchv1.Job,
) (*corev1.Secret, error) {
	secretApplyConfiguration := corev1ac.Secret(secretName, namespace).WithOwnerReferences(
		&metav1ac.OwnerReferenceApplyConfiguration{
			APIVersion: pointer.String(batchv1.SchemeGroupVersion.String()),
			Kind:       pointer.String("Job"),
			Name:       pointer.String(job.Name),
			UID:        &job.UID,
		},
	)
	secret, err := clientset.CoreV1().Secrets(namespace).Apply(ctx, secretApplyConfiguration, metav1.ApplyOptions{
		FieldManager: "cronjob-runner",
	})
	if err != nil {
		return nil, fmt.Errorf("apply error: %w", err)
	}
	log.Printf("Applied the owner reference to the Secret %s/%s", secret.Namespace, secret.Name)
	return secret, nil
}

func Delete(ctx context.Context, clientset kubernetes.Interface, namespace, secretName string) error {
	err := clientset.CoreV1().Secrets(namespace).Delete(ctx, secretName, metav1.DeleteOptions{})
	if err != nil {
		return fmt.Errorf("delete error: %w", err)
	}
	log.Printf("Created the Secret %s/%s", namespace, secretName)
	return nil
}
