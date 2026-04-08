package middleware

import (
	"fmt"
	"sync"

	"github.com/bookerbai/goclaw/pkg/errors"
)

// Registry 接口定义了 middleware 注册和获取的契约。
// 它遵循依赖倒置原则,允许 agent 包依赖抽象而非具体实现。
type Registry interface {
	// Register 注册一个 middleware 到 registry
	Register(middleware Middleware) error

	// Get 根据名称获取 middleware
	Get(name string) (Middleware, error)

	// List 返回所有已注册的 middleware
	List() []Middleware

	// MustRegister 注册 middleware,如果已存在则 panic
	MustRegister(middleware Middleware)
}

// mapRegistry 是基于 map 的简单 Registry 实现
type mapRegistry struct {
	mu    sync.RWMutex
	items map[string]Middleware
	order []string // 保持注册顺序
}

// NewRegistry 创建一个新的 middleware registry
func NewRegistry() Registry {
	return &mapRegistry{
		items: make(map[string]Middleware),
		order: make([]string, 0),
	}
}

// Register 注册一个 middleware 到 registry
func (r *mapRegistry) Register(middleware Middleware) error {
	if middleware == nil {
		return errors.ValidationError("cannot register nil middleware")
	}

	name := middleware.Name()
	if name == "" {
		return errors.ValidationError("middleware name cannot be empty")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.items[name]; exists {
		return errors.AlreadyExistsError(fmt.Sprintf("middleware %q", name))
	}

	r.items[name] = middleware
	r.order = append(r.order, name)
	return nil
}

// Get 根据名称获取 middleware
func (r *mapRegistry) Get(name string) (Middleware, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	mw, exists := r.items[name]
	if !exists {
		return nil, errors.NotFoundError(fmt.Sprintf("middleware %q", name))
	}
	return mw, nil
}

// List 返回所有已注册的 middleware,按注册顺序
func (r *mapRegistry) List() []Middleware {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]Middleware, 0, len(r.order))
	for _, name := range r.order {
		result = append(result, r.items[name])
	}
	return result
}

// MustRegister 注册 middleware,如果已存在则 panic
func (r *mapRegistry) MustRegister(middleware Middleware) {
	if err := r.Register(middleware); err != nil {
		panic(err)
	}
}
