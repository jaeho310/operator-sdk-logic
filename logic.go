/*
Copyright 2021.

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
	"time"

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	mygroupv1 "github.com/my/seminar/api/v1"
)

// HelloReconciler reconciles a Hello object
type HelloReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=mygroup.example.com,resources=hellos,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=mygroup.example.com,resources=hellos/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=mygroup.example.com,resources=hellos/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Hello object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.8.3/pkg/reconcile
func (r *HelloReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	// operator-sdk에서 제공되는틀을 제외하고는 모두 접두사로 my를 붙였습니다.

	// operator-sdk 에서 기본적으로 제공되는 로깅프레임워크입니다.
	// "github.com/go-logr/logr"
	myLogger := r.Log.WithValues("hello", req.NamespacedName)

	// cr로 정의한 객체를 가져오기위한 struct의 ref를 받아옵니다.
	myCustomResource := &mygroupv1.Hello{}

	// 데이터를 서버에서 받아와 myCustomResource에 넣어줍니다.
	err := r.Client.Get(ctx, req.NamespacedName, myCustomResource)

	// CR에 변경이 존재하거나 err가 발생한경우 진행되는 로직입니다.
	// 변경사항이 존재한다는 것으로 생각해야합니다.
	if err != nil {
		// 변경사항인 cr이 k8s에 존재하는지를 확인합니다.
		if errors.IsNotFound(err) {
			myLogger.Info("Hello cr이 삭제되었습니다.")
			return ctrl.Result{}, nil
		}
		// GET함수 에러처리
		myLogger.Error(err, "GET CR 에러발생")
		return ctrl.Result{}, err
	}

	// service객체를 만들어주고
	myService := &corev1.Service{}

	// 서버에서 cr로 만들어진 service를 받아옵니다.
	err = r.Client.Get(ctx, types.NamespacedName{
		Name:      myCustomResource.Name,
		Namespace: myCustomResource.Namespace,
	}, myService)

	// service를 받아왔더니 변경사항이 존재합니다.
	if err != nil {
		// 서비스가 found되지 않는경우 생성해줍니다.
		if errors.IsNotFound(err) {

			// 서비스생성~~ (필요한경우 arg를 받아와 넘겨줍니다. 예제에서는 하드코딩하였습니다.)
			newService := r.createService(myCustomResource)
			err = r.Create(ctx, newService)
			if err != nil {
				myLogger.Info("service 생성 실패", "svc.namespace", myCustomResource.Namespace, "svc.name", myCustomResource.Name)
				return ctrl.Result{}, err
			}
			myLogger.Info("service 생성!", "svc.namespace", myCustomResource.Namespace, "svc.name", myCustomResource.Name)
			return ctrl.Result{RequeueAfter: time.Second * 2}, nil
			// return되는 ctrl.Result의 Requeue를 true로 설정하거나
			// RequeueAfter과 시간을 지정해주면
			// 이벤트큐에 다시 올라가 다시 로직이 진행됩니다.
		}
		myLogger.Error(err, "Service를 가져오는데 실패하였습니다.")
		return ctrl.Result{}, err
	}

	// service 생성과정과 동일합니다.
	myDeployment := &appsv1.Deployment{}

	err = r.Client.Get(ctx, types.NamespacedName{
		Name:      myCustomResource.Name,
		Namespace: myCustomResource.Namespace,
	}, myDeployment)

	if err != nil {
		if errors.IsNotFound(err) {
			// Deployment 생성 로직~~~~~~~
			newDeployment := r.createDeployment(myCustomResource)
			err = r.Create(ctx, newDeployment)
			if err != nil {
				myLogger.Info("deployment 생성 실패", "dep.namespace", myCustomResource.Namespace, "dep.name", myCustomResource.Name)
				return ctrl.Result{}, err
			}
			myLogger.Info("deployment 생성!", "dep.namespace", myCustomResource.Namespace, "dep.name", myCustomResource.Name)
			return ctrl.Result{RequeueAfter: time.Second * 2}, nil
		}
		myLogger.Error(err, "deployment를 가져오는데 실패하였습니다.")
		return ctrl.Result{}, err
	}

	// Deployment를 생성할때 cr의 size로 replicaset을 생성해주었습니다.
	// 추후 cr의 filed값이 변경되어 apply된경우 해당로직으로 제어합니다.
	mySize := myCustomResource.Spec.Size

	// deployment를 정의할때 사용한 replicaset과 cr의 size가 다른경우
	if *myDeployment.Spec.Replicas != mySize {
		myDeployment.Spec.Replicas = &mySize

		// custom controller를 만들더라도 기존 k8s controll loop는 정상적으로 돌아갑니다. replicaset만 변경해서 pod 수를 제어합니다.
		myLogger.Info("replicaset(pod 사이즈) 변경!", "Replicaset.namespace", myCustomResource.Namespace, "Size", mySize)

		err = r.Client.Update(ctx, myDeployment)

		if err != nil {
			myLogger.Error(err, "replicaset 업데이트 에러", "Deployment.Namespace",
				myDeployment.Namespace, "Deployment.Name", myDeployment.Name)
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: time.Second * 2}, nil
	}
	return ctrl.Result{}, nil
}

// Service를 생성하고 컨틀롤러에 등록해 cr이 삭제된경우 함께 삭제되도록 합니다.
// yaml을 golang으로 하드코딩한 형태입니다.
func (r *HelloReconciler) createService(m *mygroupv1.Hello) *corev1.Service {

	// Label은 여러곳에서 사용하는 팟의 정보가 담긴 데이터이므로 메서드로 모듈화시켜 정적자원처럼 사용하기 위함입니다.
	myLabel := getLabelForMyCustomResource(m.Name)

	// service struct는 metadata, spec등을 구현할 수 있도록 정의되어있습니다.
	newService := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      m.Name,
			Namespace: m.Namespace,
		},
		Spec: corev1.ServiceSpec{
			Type:     corev1.ServiceTypeNodePort, // service의 타입은 NodePort입니다.
			Selector: myLabel,
			Ports: []corev1.ServicePort{
				{
					Protocol:   corev1.ProtocolTCP,
					NodePort:   31321, // 외부에서 31321포트로 접근하도록 합니다.
					Port:       8375,
					TargetPort: intstr.IntOrString{IntVal: 8395},
				},
			},
		},
	}
	// cr이 삭제됐을때 svc가 남아있는걸 막기위해 ref에 추가
	ctrl.SetControllerReference(m, newService, r.Scheme)
	return newService
}

// ****************************************************************
// CR에 Spec에 Size를 정의했던것을 사용하는 곳입니다.
// ****************************************************************
// Deployment를 생성하고 컨틀롤러에 등록해 cr이 삭제된경우 함께 삭제되도록 합니다.
// yaml을 golang으로 하드코딩한 형태입니다.
func (r *HelloReconciler) createDeployment(m *mygroupv1.Hello) *appsv1.Deployment {
	myLabel := getLabelForMyCustomResource(m.Name)

	mySize := m.Spec.Size

	newDeployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      m.Name,
			Namespace: m.Namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &mySize,
			Selector: &metav1.LabelSelector{
				MatchLabels: myLabel,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: myLabel,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Image: "repo.iris.tools/test/echoproject:4",
						Name:  "echoservice",
					}},
				},
			},
		},
	}

	ctrl.SetControllerReference(m, newDeployment, r.Scheme)
	return newDeployment
}

// pod의 Label은 pod을 identify하는 데이터이므로 메서드로 모듈화시켜 정적자원처럼 사용하기 위함입니다.
func getLabelForMyCustomResource(name string) map[string]string {
	return map[string]string{"app": "echoservice"}
}

// SetupWithManager sets up the controller with the Manager.
func (r *HelloReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&mygroupv1.Hello{}).
		Owns(&corev1.Service{}).
		Owns(&appsv1.Deployment{}).
		Complete(r)

	// For에 감시할 cr을 설정해주고
	// Owns는 서브로 감시할 대상을 설정합니다.(서브로 감시하는 대상이 삭제된경우 reconcile되도록)
	// 서브로 감시할 대상에 추가된 service와 deployment는
	// 추후 사용자가 임의로 삭제했을때 다시 복구됩니다.
	// cr이 삭제됐을때 svc와 dep가 함께 삭제되지는 않습니다. 해당로직이 필요한경우 컨트롤러 ref에 추가합니다.
}
