---
layout: home

hero:
  name: "GoClaw 源码解析"
  text: "Go 语言版 Super Agent Harness"
  tagline: 从中间件系统到 Sub-Agent 调度，从沙箱执行到 SSE 事件流，全面解读 GoClaw 的架构设计与实现细节
  actions:
    - theme: brand
      text: 开始阅读
      link: /chapters/01-what-is-goclaw
    - theme: alt
      text: 查看目录
      link: /contents
    - theme: alt
      text: GitHub
      link: https://github.com/bookerbai/goclaw

features:
  - icon:
      src: /icons/middleware.svg
    title: 中间件系统
    details: 深入 Before/After/WrapToolCall 三层钩子，解析 14 个内置中间件，理解 Agent 执行流水线的核心设计。

  - icon:
      src: /icons/subagent.svg
    title: Sub-Agent 调度
    details: 剖析 Executor 执行引擎、Task Tool、事件回调机制，掌握 Go 语言下的并发任务调度实现。

  - icon:
      src: /icons/sandbox.svg
    title: 沙箱执行环境
    details: 解读 Sandbox 接口、虚拟路径映射、Local/Docker 双实现，理解 Go 如何安全隔离执行环境。

  - icon:
      src: /icons/sse.svg
    title: SSE 事件流
    details: 覆盖 message_delta、tool_event、completed、error 四类核心事件，解析 P0 契约与客户端集成规范。
---
