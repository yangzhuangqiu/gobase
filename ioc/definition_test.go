package ioc

import (
	"context"
	"testing"

	"github.com/yangzhuangqiu/gobase/errcode"
)

type definitionService interface {
	Identity() string
}

type definitionServiceBean struct{}

// Identity 返回测试 Bean 的稳定标识，用于确认接口解析得到原始实例。
func (*definitionServiceBean) Identity() string { return "definition" }

// TestRegisterDefinitionPublishesDeclaredInterface 验证 Definition 会原样发布 Bean 与模块默认声明。
// 测试方法：构造接口声明 Definition，修改来源 options 后完成 Build，并断言解析结果仍是原始实例。
func TestRegisterDefinitionPublishesDeclaredInterface(t *testing.T) {
	bean := &definitionServiceBean{}
	options := []RegisterOption{WithDeclare[definitionService]()}
	definition := Define(bean, options...)
	// 修改来源 slice 不应污染模块已经发布的 Definition。
	options[0] = nil

	container := New()
	if err := container.RegisterDefinition(definition); err != nil {
		t.Fatalf("RegisterDefinition() error = %v", err)
	}
	if err := container.Build(context.Background()); err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	// 阶段三：按模块声明的接口解析，确认注册的是同一实例。
	service, err := ResolveAs[definitionService](container)
	if err != nil {
		t.Fatalf("ResolveAs() error = %v", err)
	}
	if service != bean || service.Identity() != "definition" {
		t.Fatalf("resolved service = %#v", service)
	}
}

// TestRegisterDefinitionRejectsEmptyBean 验证零值 Definition 复用 Register 的稳定非法 Bean 语义。
// 测试方法：注册零值 Definition，并断言错误码为 ioc.register.invalid_bean。
func TestRegisterDefinitionRejectsEmptyBean(t *testing.T) {
	container := New()
	err := container.RegisterDefinition(Definition{})
	if !errcode.IsCode[ErrorCode](err, ErrCodeIOCRegisterInvalidBean) {
		t.Fatalf("RegisterDefinition() error = %v, want code %s", err, ErrCodeIOCRegisterInvalidBean)
	}
}
