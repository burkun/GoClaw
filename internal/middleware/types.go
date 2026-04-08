package middleware

import (
	"context"

	"github.com/cloudwego/eino/components/model"
)

// ModelCreator 定义了创建 chat model 的接口
type ModelCreator func(ctx context.Context, modelName string) (model.BaseChatModel, error)

// RunConfig 定义了运行时配置接口
type RunConfig interface {
	GetModelName() string
}
