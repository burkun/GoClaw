package middleware

import (
	"context"
	"testing"
)

// mockMiddleware 是用于测试的 mock middleware
type mockMiddleware struct {
	name string
}

func (m *mockMiddleware) Name() string { return m.name }
func (m *mockMiddleware) BeforeAgent(ctx context.Context, state *State) error {
	return nil
}
func (m *mockMiddleware) BeforeModel(ctx context.Context, state *State) error {
	return nil
}
func (m *mockMiddleware) AfterModel(ctx context.Context, state *State, response *Response) error {
	return nil
}
func (m *mockMiddleware) AfterAgent(ctx context.Context, state *State, response *Response) error {
	return nil
}
func (m *mockMiddleware) WrapToolCall(ctx context.Context, state *State, toolCall *ToolCall, handler ToolHandler) (*ToolResult, error) {
	return handler(ctx, toolCall)
}

func TestRegistry_Register(t *testing.T) {
	registry := NewRegistry()

	mw1 := &mockMiddleware{name: "middleware1"}
	err := registry.Register(mw1)
	if err != nil {
		t.Errorf("Register() error = %v", err)
	}

	// 重复注册应该失败
	err = registry.Register(mw1)
	if err == nil {
		t.Error("Register() should fail for duplicate middleware")
	}

	// 注册 nil 应该失败
	err = registry.Register(nil)
	if err == nil {
		t.Error("Register() should fail for nil middleware")
	}

	// 注册空名称的 middleware 应该失败
	emptyMW := &mockMiddleware{name: ""}
	err = registry.Register(emptyMW)
	if err == nil {
		t.Error("Register() should fail for middleware with empty name")
	}
}

func TestRegistry_Get(t *testing.T) {
	registry := NewRegistry()

	mw1 := &mockMiddleware{name: "middleware1"}
	_ = registry.Register(mw1)

	// 获取已注册的 middleware
	got, err := registry.Get("middleware1")
	if err != nil {
		t.Errorf("Get() error = %v", err)
	}
	if got.Name() != "middleware1" {
		t.Error("Get() returned wrong middleware")
	}

	// 获取未注册的 middleware 应该失败
	_, err = registry.Get("not-exist")
	if err == nil {
		t.Error("Get() should fail for unregistered middleware")
	}
}

func TestRegistry_List(t *testing.T) {
	registry := NewRegistry()

	// 空 registry 应该返回空列表
	list := registry.List()
	if len(list) != 0 {
		t.Errorf("List() should return empty list, got %d items", len(list))
	}

	// 注册多个 middleware
	mw1 := &mockMiddleware{name: "middleware1"}
	mw2 := &mockMiddleware{name: "middleware2"}
	mw3 := &mockMiddleware{name: "middleware3"}

	_ = registry.Register(mw1)
	_ = registry.Register(mw2)
	_ = registry.Register(mw3)

	// 应该按注册顺序返回
	list = registry.List()
	if len(list) != 3 {
		t.Errorf("List() should return 3 items, got %d", len(list))
	}

	if list[0].Name() != "middleware1" || list[1].Name() != "middleware2" || list[2].Name() != "middleware3" {
		t.Error("List() should return middlewares in registration order")
	}
}

func TestRegistry_MustRegister(t *testing.T) {
	registry := NewRegistry()

	mw1 := &mockMiddleware{name: "middleware1"}
	// 应该不 panic
	registry.MustRegister(mw1)

	// 重复注册应该 panic
	defer func() {
		if r := recover(); r == nil {
			t.Error("MustRegister() should panic for duplicate middleware")
		}
	}()
	registry.MustRegister(mw1)
}
