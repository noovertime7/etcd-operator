package controllers

import (
	"context"
	"fmt"
	"reflect"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// 定义执行动作接口

type Action interface {
	Execute(ctx context.Context) error
}

type PatchStatus struct {
	Client client.Client
	old    client.Object
	new    client.Object
}

func (p *PatchStatus) Execute(ctx context.Context) error {
	if reflect.DeepEqual(p.old, p.new) {
		return nil
	}
	//更新状态
	if err := p.Client.Status().Patch(ctx, p.new, client.MergeFrom(p.old)); err != nil {
		return fmt.Errorf("patching status error %s", err)
	}
	return nil
}

//创建新资源对象

type CreateObj struct {
	client client.Client
	obj    client.Object
}

func (c *CreateObj) Execute(ctx context.Context) error {
	return c.client.Create(ctx, c.obj)
}
