package eino

import (
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

// Message 是 GoClaw 对 Eino 消息类型的别名。
type Message = *schema.Message

// AgentEvent 是 GoClaw 对 Eino AgentEvent 的别名。
type AgentEvent = adk.AgentEvent

// ResumeParams 是 GoClaw 对 Eino 恢复参数的别名。
type ResumeParams = adk.ResumeParams

// AgentRunOption 是 GoClaw 对 Eino 运行选项的别名。
type AgentRunOption = adk.AgentRunOption
