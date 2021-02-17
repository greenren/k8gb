package controllers

import (
	"context"

	k8gbv1beta1 "github.com/AbsaOSS/k8gb/api/v1beta1"
	v1beta1 "k8s.io/api/extensions/v1beta1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

func (r *GslbReconciler) gslbIngress(gslb *k8gbv1beta1.Gslb) (*v1beta1.Ingress, error) {
	if gslb.Annotations == nil {
		gslb.Annotations = make(map[string]string)
	}
	gslb.Annotations[strategyAnnotation] = gslb.Spec.Strategy.Type
	if gslb.Spec.Strategy.PrimaryGeoTag != "" {
		gslb.Annotations[primaryGeoTagAnnotation] = gslb.Spec.Strategy.PrimaryGeoTag
	}
	ingress := &v1beta1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:        gslb.Name,
			Namespace:   gslb.Namespace,
			Annotations: gslb.Annotations,
		},
		Spec: gslb.Spec.Ingress,
	}

	err := controllerutil.SetControllerReference(gslb, ingress, r.Scheme)
	if err != nil {
		return nil, err
	}
	return ingress, err
}

func (r *GslbReconciler) saveIngress(instance *k8gbv1beta1.Gslb, i *v1beta1.Ingress) error {
	found := &v1beta1.Ingress{}
	err := r.Get(context.TODO(), types.NamespacedName{
		Name:      instance.Name,
		Namespace: instance.Namespace,
	}, found)
	if err != nil && errors.IsNotFound(err) {

		// Create the service
		log.Info("Creating a new Ingress", "Ingress.Namespace", i.Namespace, "Ingress.Name", i.Name)
		err = r.Create(context.TODO(), i)

		if err != nil {
			// Creation failed
			log.Error(err, "Failed to create new Ingress", "Ingress.Namespace", i.Namespace, "Ingress.Name", i.Name)
			return err
		}
		// Creation was successful
		return nil
	} else if err != nil {
		// Error that isn't due to the service not existing
		log.Error(err, "Failed to get Ingress")
		return err
	}

	// Update existing object with new spec and annotations
	found.Spec = i.Spec
	found.Annotations = i.Annotations
	err = r.Update(context.TODO(), found)

	if err != nil {
		// Update failed
		log.Error(err, "Failed to update Ingress", "Ingress.Namespace", found.Namespace, "Ingress.Name", found.Name)
		return err
	}

	return nil
}
