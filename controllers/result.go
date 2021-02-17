package controllers

import (
	"time"

	"github.com/AbsaOSS/k8gb/controllers/depresolver"

	"github.com/go-logr/logr"
	ctrl "sigs.k8s.io/controller-runtime"
)

type ReconcileResultHandler struct {
	log           logr.Logger
	delayedResult ctrl.Result
}

func NewReconcileResultHandler(config depresolver.Config, log logr.Logger) *ReconcileResultHandler {
	return &ReconcileResultHandler{
		delayedResult: ctrl.Result{RequeueAfter: time.Second * time.Duration(config.ReconcileRequeueSeconds)},
		log:           log,
	}
}

// Stop stops reconciliation loop
func (r *ReconcileResultHandler) Stop() (ctrl.Result, error) {
	r.log.Info("reconciler exit")
	return ctrl.Result{}, nil
}

// RequeueDelayWithError requeue loop after config.ReconcileRequeueSeconds
func (r *ReconcileResultHandler) RequeueDelayWithError(err error) (ctrl.Result, error) {
	r.log.Error(err, "reconciler error")
	return r.delayedResult, nil
}

// RequeueDelay requeue loop after config.ReconcileRequeueSeconds
func (r *ReconcileResultHandler) RequeueDelay() (ctrl.Result, error) {
	return r.delayedResult, nil
}

// RequeueNowWithError requeue loop immediately
func (r *ReconcileResultHandler) RequeueNowWithError(err error) (ctrl.Result, error) {
	// error is handled in caller function
	return ctrl.Result{}, err
}
