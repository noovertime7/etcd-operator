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
	"fmt"
	"html/template"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"strings"
	"time"

	etcdv1alpha1 "github.com/noovertime7/etcd-operator/api/v1alpha1"
)

// EtcdBackupReconciler reconciles a EtcdBackup object
type EtcdBackupReconciler struct {
	Recorder record.EventRecorder
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
	pod *corev1.Pod
}

// controllers/etcdbackup_controller.go

// setStateActual 用于设置 backupState 的真实状态
func (r *EtcdBackupReconciler) setStateActual(ctx context.Context, state *backupState) error {
	var actual backupStateContainer

	key := client.ObjectKey{
		Name:      state.backup.Name,
		Namespace: state.backup.Namespace,
	}

	// 获取对应的 Pod
	actual.pod = &corev1.Pod{}
	if err := r.Get(ctx, key, actual.pod); err != nil {
		if client.IgnoreNotFound(err) != nil {
			return fmt.Errorf("getting pod error: %s", err)
		}
		actual.pod = nil
	}

	// 填充当前真实的状态
	state.actual = &actual
	return nil
}

// setStateDesired 用于设置 backupState 的期望状态（根据 EtcdBackup 对象）
func (r *EtcdBackupReconciler) setStateDesired(state *backupState) error {
	var desired backupStateContainer

	// 创建一个管理的 Pod 用于执行备份操作
	pod, err := podForBackup(state.backup)
	if err != nil {
		return fmt.Errorf("computing pod for backup error: %q", err)
	}
	// 配置 controller reference
	if err := controllerutil.SetControllerReference(state.backup, pod, r.Scheme); err != nil {
		return fmt.Errorf("setting pod controller reference error : %s", err)
	}
	desired.pod = pod
	// 获得期望的对象
	state.desired = &desired
	return nil
}

// getState 用来获取当前应用的整个状态，然后才方便判断下一步动作
func (r EtcdBackupReconciler) getState(ctx context.Context, req ctrl.Request) (*backupState, error) {
	var state backupState

	// 获取 EtcdBackup 对象
	state.backup = &etcdv1alpha1.EtcdBackup{}
	if err := r.Get(ctx, req.NamespacedName, state.backup); err != nil {
		if client.IgnoreNotFound(err) != nil {
			return nil, fmt.Errorf("getting backup error: %s", err)
		}
		// 被删除了则直接忽略
		state.backup = nil
		return &state, nil
	}

	// 获取当前备份的真实状态
	if err := r.setStateActual(ctx, &state); err != nil {
		return nil, fmt.Errorf("setting actual state error: %s", err)
	}

	// 获取当前期望的状态
	if err := r.setStateDesired(&state); err != nil {
		return nil, fmt.Errorf("setting desired state error: %s", err)
	}

	return &state, nil
}

// podForBackup 创建一个 Pod 运行备份任务
func podForBackup(backup *etcdv1alpha1.EtcdBackup) (*corev1.Pod, error) {
	var secretRef *corev1.SecretEnvSource
	var endpoint, backupUrl string
	switch {
	case backup.Spec.StorageType == etcdv1alpha1.EtcdBackupStorageTypeS3:
		endpoint = backup.Spec.S3.EndPoint
		//模板化path
		tmpl, err := template.New("template").Parse(backup.Spec.S3.Path)
		if err != nil {
			return nil, err
		}
		// 解析成备份的地址
		var objectURL strings.Builder
		err = tmpl.Execute(&objectURL, backup)
		if err != nil {
			return nil, err
		}
		fmt.Println("objectURL = ", objectURL.String())
		backupUrl = fmt.Sprintf("%s://%s", backup.Spec.StorageType, objectURL.String())
		secretRef = &corev1.SecretEnvSource{
			LocalObjectReference: corev1.LocalObjectReference{
				Name: backup.Spec.S3.Secret,
			},
		}
	case backup.Spec.StorageType == etcdv1alpha1.EtcdBackupStorageTypeOss:
		endpoint = backup.Spec.OSS.EndPoint
		backupUrl = fmt.Sprintf("%s://%s", backup.Spec.StorageType, backup.Spec.OSS.Path)
		secretRef = &corev1.SecretEnvSource{
			LocalObjectReference: corev1.LocalObjectReference{
				Name: backup.Spec.OSS.Secret,
			},
		}
	default:
		secretRef = &corev1.SecretEnvSource{
			LocalObjectReference: corev1.LocalObjectReference{
				Name: backup.Spec.S3.Secret,
			},
		}
	}

	// 构造一个全新的备份 Pod
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      backup.Name,
			Namespace: backup.Namespace,
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  "backup-agent",
					Image: backup.Spec.Image, // todo，执行备份的镜像
					Args: []string{
						"--etcd-url", backup.Spec.EtcdUrl,
						"--backup-url", backupUrl,
					},
					Env: []corev1.EnvVar{{
						Name:  "ENDPOINT",
						Value: endpoint,
					}},
					EnvFrom: []corev1.EnvFromSource{
						{
							SecretRef: secretRef,
						},
					},
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("100m"),
							corev1.ResourceMemory: resource.MustParse("50Mi"),
						},
						Limits: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("100m"),
							corev1.ResourceMemory: resource.MustParse("50Mi"),
						},
					},
				},
			},
			RestartPolicy: corev1.RestartPolicyNever,
		},
	}, nil
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
	case state.backup == nil: // 被删除了
		logger.Info("Backup Object not found. Ignoring.")
	case !state.backup.DeletionTimestamp.IsZero(): // 标记为了删除
		logger.Info("Backup Object has been deleted. Ignoring.")
	case state.backup.Status.Phase == "": // 开始备份，更新状态
		logger.Info("Backup Staring. Updating status.")
		newBackup := state.backup.DeepCopy() // 深拷贝一份
		newBackup.Status.Phase = etcdv1alpha1.EtcdBackupPhaseBackingUp
		state.backup.Status.CompletionTime = &metav1.Time{Time: time.Now()}
		// 更新状态为备份中
		action = &PatchStatus{client: r.Client, original: state.backup, new: newBackup} // 下一步要执行的动作

	case state.backup.Status.Phase == etcdv1alpha1.EtcdBackupPhaseFailed: // 备份失败
		logger.Info("Backup has failed. Ignoring.")
	case state.backup.Status.Phase == etcdv1alpha1.EtcdBackupPhaseCompleted: // 备份完成
		logger.Info("Backup has completed. Ignoring.")

	case state.actual.pod == nil: // 当前还没有备份的 Pod
		logger.Info("Backup Pod does not exists. Creating.")
		action = &CreateObject{client: r.Client, obj: state.desired.pod} // 下一步要执行的动作
		r.Recorder.Event(state.backup, corev1.EventTypeNormal, EventReasonSuccessfulCreate, "Success for create backup pod")

	case state.actual.pod.Status.Phase == corev1.PodFailed: // 备份Pod执行失败
		logger.Info("Backup Pod failed. Updating status.")
		newBackup := state.backup.DeepCopy()
		newBackup.Status.Phase = etcdv1alpha1.EtcdBackupPhaseFailed
		action = &PatchStatus{client: r.Client, original: state.backup, new: newBackup} // 下一步更新状态为失败

		r.Recorder.Event(state.backup, corev1.EventTypeWarning, EventReasonBackupFailed, "Backup failed,See backup pod for detail information")

	case state.actual.pod.Status.Phase == corev1.PodSucceeded: // 备份Pod执行完成
		logger.Info("Backup Pod succeeded. Updating status.")
		newBackup := state.backup.DeepCopy()
		newBackup.Status.Phase = etcdv1alpha1.EtcdBackupPhaseCompleted
		action = &PatchStatus{client: r.Client, original: state.backup, new: newBackup} // 下一步更新状态为完成

		r.Recorder.Event(state.backup, corev1.EventTypeNormal, EventReasonBackupSuccess, "Backup completed successfully")
	}

	// 执行动作
	if action != nil {
		if err := action.Execute(ctx); err != nil {
			return ctrl.Result{}, fmt.Errorf("executing action error: %s", err)
		}
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *EtcdBackupReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&etcdv1alpha1.EtcdBackup{}).
		Owns(&corev1.Pod{}).
		Complete(r)
}
