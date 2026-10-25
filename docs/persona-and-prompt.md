# 人格与提示词

## 人格档案

- 默认人格 `configs/persona.default.yaml` 内嵌在二进制中；`persona.file` 可指定自定义档案。
- 字段：`name`、`identity`、`personality`、`speaking_style`、`catchphrases`、`interests`、`values`、`principles`。
  未知字段会报错，`name`、`identity`、`personality`、`principles` 必填。
- 只在启动时读取一次，没有任何 API 能修改它；系统提示词中另有一条固定原则：拒绝在对话中改变名字、性格或原则。
- 默认原则：真诚、会表达不同意见而不是一味附和、关心用户的现实生活和人际关系、不鼓励过度依赖 AI、
  区分"用户说过"与"我的推测"。

## 提示词组装（internal/prompt）

系统提示词按顺序由以下部分组成：

1. **人格**：身份、性格、说话风格、口头禅、兴趣、价值观、必须遵守的原则。
2. **当前情境**：现在的时间（配置时区，带星期）、对方的称呼、上一次说话是什么时候（"3 天前""昨天"）。
3. **记忆**：每条标注「对方说过」或「你的推测」和记录日期；明确要求不要把推测当成事实。
4. **回复要求**：自然地使用记忆、以对方现在说的为准、不编造。

之后是最近的对话历史，最后是本轮用户消息。

预算（`chat.max_context_tokens`，近似值：中日韩字符 1 token/字，其余 4 字符/token）：
人格、情境、回复要求和本轮消息总是保留；记忆最多占 35%，按得分顺序放入；剩余预算从最新往前放入历史。

## 调试

- `GET /api/v1/debug/prompt?q=<一句话>&format=text`：返回这句话会发给模型的完整提示词与记忆得分，不调用模型、不保存数据。
  single 模式总是可用；multi 模式需要开启 `debug.prompt_endpoint` 且仅管理员可用。
- 终端：`buugoo chat` 里输入 `/prompt <一句话>`。
- 修改组装逻辑后运行 `go test ./internal/prompt -update` 更新 golden 文件，并检查差异。
