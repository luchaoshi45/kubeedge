package sqlite

import (
	"context"
	"fmt"
	"reflect"
	"strconv"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/conversion"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/apiserver/pkg/storage"
	"k8s.io/klog/v2"

	"github.com/kubeedge/kubeedge/edge/pkg/metamanager/metaserver/kubernetes/storage/sqlite/imitator"
	"github.com/kubeedge/kubeedge/pkg/metaserver"
	"github.com/kubeedge/kubeedge/pkg/metaserver/util"
)

/*
This file is designed to encapsulate the Imitator as Store.Interface,
*/
type store struct {
	client    imitator.Client
	versioner storage.Versioner
	codec     runtime.Codec
	watcher   *watcher
}

func (s *store) Versioner() storage.Versioner {
	return s.versioner
}

func (s *store) Create(context.Context, string, runtime.Object, runtime.Object, uint64) error {
	panic("Do not call this function")
}

func (s *store) Delete(context.Context, string, runtime.Object, *storage.Preconditions,
	storage.ValidateObjectFunc, runtime.Object) error {
	panic("Do not call this function")
}

func (s *store) Watch(ctx context.Context, key string, opts storage.ListOptions) (watch.Interface, error) {
	return s.watch(ctx, key, opts, false)
}

func (s *store) watch(ctx context.Context, key string, opts storage.ListOptions, recursive bool) (watch.Interface, error) {
	rev, err := s.versioner.ParseResourceVersion(opts.ResourceVersion)
	if err != nil {
		return nil, err
	}
	return s.watcher.Watch(ctx, key, int64(rev), recursive, opts.Predicate)
}

func (s *store) Get(_ context.Context, key string, _ storage.GetOptions, objPtr runtime.Object) error {
	resp, err := s.client.Get(context.TODO(), key)
	if err != nil || len(*resp.Kvs) == 0 {
		klog.Error(err)
		return err
	}
	unstrObj := objPtr.(*unstructured.Unstructured)
	return runtime.DecodeInto(s.codec, []byte((*resp.Kvs)[0].Value), unstrObj)
}

func (s *store) GetList(_ context.Context, key string, opts storage.ListOptions, listObj runtime.Object) error {
	klog.Infof("get a list req, key=%v", key)
	listPtr, err := meta.GetItemsPtr(listObj)
	if err != nil {
		return err
	}
	v, err := conversion.EnforcePtr(listPtr)
	if err != nil || v.Kind() != reflect.Slice {
		return fmt.Errorf("need ptr to slice: %v", err)
	}

	resp, err := s.client.List(context.TODO(), key)

	if err != nil || len(*resp.Kvs) == 0 {
		klog.Error(err)
		return err
	}
	unstrList := listObj.(*unstructured.UnstructuredList)
	for _, v := range *resp.Kvs {
		var unstrObj unstructured.Unstructured
		err := runtime.DecodeInto(s.codec, []byte(v.Value), &unstrObj)
		if err != nil {
			return err
		}

		labelSet := labels.Set(unstrObj.GetLabels())
		if !opts.Predicate.Label.Matches(labelSet) {
			continue
		}

		// only support metadata.name & metadata.namespace
		fieldSet := fields.Set{
			"metadata.name":      unstrObj.GetName(),
			"metadata.namespace": unstrObj.GetNamespace(),
		}
		if !opts.Predicate.Field.Matches(fieldSet) {
			continue
		}

		unstrList.Items = append(unstrList.Items, unstrObj)
	}
	rv := strconv.FormatUint(resp.Revision, 10)
	unstrList.SetResourceVersion(rv)
	unstrList.SetSelfLink(key)
	gvr, _, _ := metaserver.ParseKey(key)
	unstrList.SetGroupVersionKind(gvr.GroupVersion().WithKind(util.UnsafeResourceToKind(gvr.Resource) + "List"))
	return nil
}

func (s *store) GuaranteedUpdate(context.Context, string, runtime.Object, bool, *storage.Preconditions, storage.UpdateFunc, runtime.Object) error {
	panic("Do not call this function")
}

func (s *store) Count(string) (int64, error) {
	panic("implement me")
}

func (s *store) RequestWatchProgress(context.Context) error {
	panic("Do not call this function")
}

func New() storage.Interface {
	return newStore()
}
func newStore() *store {
	codec := unstructured.UnstructuredJSONScheme
	client := imitator.DefaultV2Client
	s := store{
		client:    client,
		versioner: imitator.Versioner,
		watcher:   newWatcher(client, codec),
		codec:     codec,
	}
	return &s
}
