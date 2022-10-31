/*
Copyright 2022 chenteng.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"context"
	"errors"
	"fmt"
	coreV1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	etcdv1alpha1 "github.com/noovertime7/etcd-operator/api/v1alpha1"
)

// EtcdBackupReconciler reconciles a EtcdBackup object
type EtcdBackupReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=etcd.yunxue521.top,resources=etcdbackups,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=pods/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=etcd.yunxue521.top,resources=etcdbackups/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=etcd.yunxue521.top,resources=etcdbackups/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the EtcdBackup object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.12.2/pkg/reconcile

type backupState struct {
	backup  *etcdv1alpha1.EtcdBackup
	actual  *backupStateContainer
	desired *backupStateContainer // 期望的状态
}

type backupStateContainer struct {
	pod *coreV1.Pod
}

// 获取真实的状态
func (r *EtcdBackupReconciler) setStateActual(ctx context.Context, stat *backupState) error {
	var actual backupStateContainer
	key := client.ObjectKey{
		Namespace: stat.backup.Namespace,
		Name:      stat.backup.Name,
	}
	//获取对应的POd
	actual.pod = &coreV1.Pod{}
	if err := r.Get(ctx, key, actual.pod); err != nil {
		if client.IgnoreNotFound(err) != nil {
			return fmt.Errorf("getting pod err %v", err)
		}
		actual.pod = nil
	}
	//填充真实的状态
	stat.actual = &actual
	return nil
}

//获取期望的状态

func (r *EtcdBackupReconciler) setStateDesired(ctx context.Context, state *backupState) error {
	var desired backupStateContainer
	//根据etcdbackup 创建一个用于备份etcd的pod
	pod := podForBackup(state.backup)

	//配置controller own
	if err := controllerutil.SetControllerReference(state.backup, pod, r.Scheme); err != nil {
		return fmt.Errorf("setting controller reference error")
	}

	desired.pod = pod

	state.desired = &desired
	return nil
}

//获取当前应用的整个状态

func (r *EtcdBackupReconciler) getState(ctx context.Context, req ctrl.Request) (*backupState, error) {
	var state backupState
	state.backup = &etcdv1alpha1.EtcdBackup{}
	if err := r.Get(ctx, req.NamespacedName, state.backup); err != nil {
		if client.IgnoreNotFound(err) != nil {
			return nil, fmt.Errorf("getting backupobj error")
		}
		//被删除了直接忽略
		state.backup = nil
	}
	//获得了etcdbackup对象

	//获取当前真实的状态
	if err := r.setStateActual(ctx, &state); err != nil {
		return nil, fmt.Errorf("setting actual state error %v", err)
	}
	//获取期望的状态
	if err := r.setStateDesired(ctx, &state); err != nil {
		return nil, fmt.Errorf("setting Desired state error %v", err)
	}
	return &state, nil
}

func podForBackup(backup *etcdv1alpha1.EtcdBackup) *coreV1.Pod {
	return &coreV1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: backup.Namespace,
			Name:      backup.Namespace,
		},
		Spec: coreV1.PodSpec{
			Containers: []coreV1.Container{
				{
					Name:            "etcd-backup",
					ImagePullPolicy: coreV1.PullIfNotPresent,
					Image:           backup.Spec.Image,
					Resources: coreV1.ResourceRequirements{
						Requests: coreV1.ResourceList{
							coreV1.ResourceCPU:    resource.MustParse("100m"),
							coreV1.ResourceMemory: resource.MustParse("100Mi"),
						},
						Limits: coreV1.ResourceList{
							coreV1.ResourceCPU:    resource.MustParse("100m"),
							coreV1.ResourceMemory: resource.MustParse("100Mi"),
						},
					},
				},
			},
			RestartPolicy: coreV1.RestartPolicyNever,
		},
	}
}

func (r *EtcdBackupReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	state, err := r.getState(ctx, req)
	if err != nil {
		return ctrl.Result{}, err
	}
	// 根据状态来判断下一步执行的动作
	var action Action
	//开始判断状态
	switch {
	case state.backup == nil: //被删除了
		logger.Info("Backup Object not found")
	case state.backup.DeletionTimestamp.IsZero(): //被标记为删除了
		logger.Info("Backup Object has been deleted")
	case state.backup.Status.Phase == "": //开始备份,先标记为备份中
		logger.Info("Backup starting...")
		newBackup := state.backup.DeepCopy()
		newBackup.Status.Phase = etcdv1alpha1.EtcdBackupPhaseBackingUp
		action = &PatchStatus{
			Client: r.Client,
			old:    state.backup,
			new:    newBackup,
		}
	case state.backup.Status.Phase == etcdv1alpha1.EtcdBackupPhaseFailed:
		logger.Error(errors.New("backup Has failed"), "Backup Has failed")
	case state.backup.Status.Phase == etcdv1alpha1.EtcdBackupPhaseCompleted:
		logger.Info("Backup has complected,Ignoring...")
	case state.actual.pod == nil:
		logger.Info("Backup Pod does not exists,Creating...")
		action = &CreateObj{obj: state.desired.pod, client: r.Client}
	case state.actual.pod.Status.Phase == coreV1.PodFailed:
		logger.Error(errors.New("Backup failed"), "Backup failed")
		newbackup := state.backup.DeepCopy()
		newbackup.Status.Phase = etcdv1alpha1.EtcdBackupPhaseFailed
		action = &PatchStatus{Client: r.Client, new: newbackup, old: state.backup}

	case state.actual.pod.Status.Phase == coreV1.PodSucceeded:
		logger.Info("Backup Pod Success")
		newBackup := state.backup.DeepCopy()
		newBackup.Status.Phase = etcdv1alpha1.EtcdBackupPhaseCompleted // 备份成功
		action = &PatchStatus{Client: r.Client, new: newBackup, old: state.backup}
	}

	if action != nil {
		if err := action.Execute(ctx); err != nil {
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *EtcdBackupReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&etcdv1alpha1.EtcdBackup{}).
		Owns(&coreV1.Pod{}).
		Complete(r)
}
