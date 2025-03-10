/*
Copyright 2022.

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

package v1

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// log is for logging in this package.
var imagelog = logf.Log.WithName("image-resource")

func (i *Image) SetupWebhookWithManager(mgr ctrl.Manager) error {
	m := &ImageMutator{}
	v := &ImageValidator{Client: mgr.GetClient()}
	return ctrl.NewWebhookManagedBy(mgr).
		For(i).
		WithDefaulter(m).
		WithValidator(v).
		Complete()
}

//+kubebuilder:webhook:path=/mutate-imagehub-sealos-io-v1-image,mutating=true,failurePolicy=fail,sideEffects=None,groups=imagehub.sealos.io,resources=images,verbs=create;update,versions=v1,name=mimage.kb.io,admissionReviewVersions=v1

type ImageMutator struct {
}

func (m *ImageMutator) Default(ctx context.Context, obj runtime.Object) error {
	img, ok := obj.(*Image)
	if !ok {
		return errors.New("obj convert Image is error")
	}
	imagelog.Info("default", "name", img.Name)
	img.ObjectMeta = initAnnotationAndLabels(img.ObjectMeta)
	img.ObjectMeta.Labels[SealosOrgLable] = img.Spec.Name.GetOrg()
	img.ObjectMeta.Labels[SealosRepoLabel] = img.Spec.Name.GetRepo()
	img.ObjectMeta.Labels[SealosTagLabel] = img.Spec.Name.GetTag()
	return nil
}

//+kubebuilder:webhook:path=/validate-imagehub-sealos-io-v1-image,mutating=false,failurePolicy=fail,sideEffects=None,groups=imagehub.sealos.io,resources=images,verbs=create;update;delete,versions=v1,name=vimage.kb.io,admissionReviewVersions=v1
//+kubebuilder:object:generate=false

// ImageValidator will validate Images change.
type ImageValidator struct {
	client.Client
}

func (v *ImageValidator) ValidateCreate(ctx context.Context, obj runtime.Object) error {
	i, ok := obj.(*Image)
	if !ok {
		return errors.New("obj convert Image is error")
	}
	imagelog.Info("validating create", "name", i.Name)
	imagelog.Info("enter checkOption func", "name", i.Name)
	return checkOption(ctx, imagelog, v.Client, i)
}

func (v *ImageValidator) ValidateUpdate(ctx context.Context, oldObj, newObj runtime.Object) error {
	ni, ok := newObj.(*Image)
	if !ok {
		return errors.New("obj convert Image is error")
	}
	oi, ok := oldObj.(*Image)
	if !ok {
		return errors.New("obj convert Image is error")
	}
	imagelog.Info("validating update", "name", oi.Name)
	if ni.Spec.Name != oi.Spec.Name {
		return fmt.Errorf("can not change spec.name: %s", string(ni.Spec.Name))
	}
	imagelog.Info("enter checkOption func", "name", ni.Name)
	return checkOption(ctx, imagelog, v.Client, ni)
}

func (v *ImageValidator) ValidateDelete(ctx context.Context, obj runtime.Object) error {
	i, ok := obj.(*Image)
	if !ok {
		return errors.New("obj convert Image is error")
	}
	imagelog.Info("validating delete", "name", i.Name)
	imagelog.Info("enter checkOption func", "name", i.Name)
	return checkOption(ctx, imagelog, v.Client, i)
}

func checkOption(ctx context.Context, logger logr.Logger, c client.Client, i Checker) error {
	logger.Info("checking label and spec name", "obj name", i.getSpecName())
	if !i.checkLabels() || !i.checkSpecName() {
		return fmt.Errorf("missing labels or obj.Spec.Name is IsLegal: %s", i.getSpecName())
	}
	logger.Info("getting org", "org", i.getSpecName())
	org := &Organization{}
	if err := c.Get(ctx, client.ObjectKey{Name: i.getOrgName()}, org); err != nil {
		if client.IgnoreNotFound(err) == nil {
			return fmt.Errorf("organization not exited %s", i.getOrgName())
		}
		return fmt.Errorf("get Organization error %s", i.getOrgName())
	}
	logger.Info("org info", "org", org)
	logger.Info("getting req from ctx")
	req, err := admission.RequestFromContext(ctx)
	if err != nil {
		logger.Info("get request from context error when validate", "obj name", i.getName())
		return err
	}
	logger.Info("checking user", "user", req.UserInfo.Username)
	// if sa is in system:serviceaccount:imagehub-system, pass it.
	imagehubNameSpacePrefix := fmt.Sprintf("%s:%s:", saPrefix, getImagehubNamespace())
	if strings.HasPrefix(req.UserInfo.Username, imagehubNameSpacePrefix) {
		logger.Info("pass for imagehub controller")
		return nil
	}
	// get sa namespace prefix, prefix format is like: "system:serviceaccount:user-system:"
	userNamespacePrefix := fmt.Sprintf("%s:%s:", saPrefix, getUserNamespace())
	// req.UserInfo.Username e.g: system:serviceaccount:user-system:labring
	if !strings.HasPrefix(req.UserInfo.Username, userNamespacePrefix) {
		return fmt.Errorf("denied, you are not one of user in %s", userNamespacePrefix)
	}
	// replace it and compare
	userName := strings.Replace(req.UserInfo.Username, userNamespacePrefix, "", -1)
	logger.Info("checking username", "user", userName)
	for _, usr := range org.Spec.Manager {
		if usr == userName {
			return nil
		}
	}
	logger.Info("denied", "obj name", i.getName())
	return fmt.Errorf("denied, you are not one of organization %s managers", i.getOrgName())
}

func getUserNamespace() string {
	userNamespace := os.Getenv("USER_NAMESPACE")
	if userNamespace == "" {
		return defaultUserNamespace
	}
	return userNamespace
}
func getImagehubNamespace() string {
	imagehubNamespace := os.Getenv("IMAGEHUB_NAMESPACE")
	if imagehubNamespace == "" {
		return defaultImagehubNamespace
	}
	return imagehubNamespace
}
