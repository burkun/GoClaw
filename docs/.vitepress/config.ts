import { defineConfig } from 'vitepress'

export default defineConfig({
  title: 'GoClaw 源码解析',
  description: 'Go 语言版 Super Agent Harness 深度解析',
  lang: 'zh-CN',

  base: '/',
  ignoreDeadLinks: true,

  head: [
    ['link', { rel: 'icon', type: 'image/svg+xml', href: '/favicon.svg' }],
    ['meta', { name: 'theme-color', content: '#00ADD8' }],
    ['meta', { property: 'og:type', content: 'website' }],
    ['meta', { property: 'og:title', content: 'GoClaw 源码解析' }],
    ['meta', { property: 'og:description', content: 'Go 语言版 Super Agent Harness 深度解析' }],
  ],

  themeConfig: {
    logo: { src: '/logo.png', alt: 'GoClaw' },

    nav: [
      { text: '开始阅读', link: '/chapters/01-what-is-goclaw' },
      { text: '目录', link: '/contents' },
      { text: 'GitHub', link: 'https://github.com/bookerbai/goclaw' },
    ],

    sidebar: [
      {
        text: '前言',
        items: [
          { text: '关于本书', link: '/' },
          { text: '完整目录', link: '/contents' },
        ],
      },
      {
        text: '第一部分：宏观认知',
        collapsed: false,
        items: [
          { text: '第 1 章　GoClaw 是什么', link: '/chapters/01-what-is-goclaw' },
          { text: '第 2 章　项目结构与技术栈', link: '/chapters/02-project-structure' },
          { text: '第 3 章　快速上手', link: '/chapters/03-quick-start' },
        ],
      },
      {
        text: '第二部分：核心引擎',
        collapsed: false,
        items: [
          { text: '第 4 章　Eino 框架：Go 的 Agent 编排', link: '/chapters/04-eino-framework' },
          { text: '第 5 章　Lead Agent：核心循环', link: '/chapters/05-lead-agent' },
          { text: '第 6 章　中间件系统', link: '/chapters/06-middleware-system' },
          { text: '第 7 章　State 与 Reducer', link: '/chapters/07-state-reducer' },
        ],
      },
      {
        text: '第三部分：Sub-Agent 系统',
        collapsed: false,
        items: [
          { text: '第 8 章　Sub-Agent 架构总览', link: '/chapters/08-subagent-overview' },
          { text: '第 9 章　Executor 执行引擎', link: '/chapters/09-executor' },
          { text: '第 10 章　Task Tool 与事件流', link: '/chapters/10-task-tool' },
        ],
      },
      {
        text: '第四部分：沙箱与工具',
        collapsed: false,
        items: [
          { text: '第 11 章　Sandbox 抽象层', link: '/chapters/11-sandbox-abstraction' },
          { text: '第 12 章　工具系统', link: '/chapters/12-tools-system' },
          { text: '第 13 章　MCP 集成', link: '/chapters/13-mcp-integration' },
        ],
      },
      {
        text: '第五部分：记忆与上下文',
        collapsed: false,
        items: [
          { text: '第 14 章　长期记忆系统', link: '/chapters/14-memory-system' },
          { text: '第 15 章　上下文工程', link: '/chapters/15-context-engineering' },
        ],
      },
      {
        text: '第六部分：Gateway 与通信',
        collapsed: false,
        items: [
          { text: '第 16 章　Gateway HTTP API', link: '/chapters/16-gateway-api' },
          { text: '第 17 章　SSE 事件协议', link: '/chapters/17-sse-protocol' },
          { text: '第 18 章　IM 渠道系统', link: '/chapters/18-im-channels' },
        ],
      },
      {
        text: '第七部分：配置与部署',
        collapsed: false,
        items: [
          { text: '第 19 章　配置体系', link: '/chapters/19-config-system' },
          { text: '第 20 章　模型配置与适配', link: '/chapters/20-model-config' },
          { text: '第 21 章　部署与生产化', link: '/chapters/21-deployment' },
        ],
      },
      {
        text: '附录',
        collapsed: true,
        items: [
          { text: '附录 A：API 契约', link: '/chapters/appendix-a-api-contract' },
          { text: '附录 B：事件类型速查', link: '/chapters/appendix-b-events-reference' },
          { text: '附录 C：术语表', link: '/chapters/appendix-c-glossary' },
        ],
      },
    ],

    outline: {
      level: [2, 3],
      label: '本页目录',
    },

    socialLinks: [
      { icon: 'github', link: 'https://github.com/bookerbai/goclaw' },
    ],

    footer: {
      message: '基于 MIT 协议发布',
      copyright: 'Copyright © 2025-present',
    },

    search: {
      provider: 'local',
    },
  },

  markdown: {
    lineNumbers: true,
  },
})
