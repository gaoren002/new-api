# Qwen3Guard 审核模型渠道与审核 API

Qwen3Guard 渠道（渠道类型 61）将 Qwen3Guard-Gen 安全分类模型作为独立的审核模型对外提供服务。上游可以是任何以 OpenAI 兼容 `/v1/chat/completions` 方式部署 Qwen3Guard-Gen 的服务（Ollama、vLLM、DashScope 兼容模式等）。渠道、令牌、分组、计费、日志与其他模型完全一致。

## 渠道配置

1. 在控制台新建渠道，类型选择 **Qwen3Guard**。
2. Base URL 填写上游地址，例如本地 Ollama 为 `http://localhost:11434`（留空使用该默认值）。密钥可选，本地 Ollama 通常留空。
3. 模型列表默认提供 `qwen3guard`、`qwen3guard-gen-0.6b`、`qwen3guard-gen-4b`、`qwen3guard-gen-8b`。如果上游模型名不同（例如 Ollama 的 `sileader/qwen3guard:0.6b`），使用渠道模型映射把对外模型名映射到上游模型名，或直接把上游模型名加入模型列表。
4. 分组与其他渠道一致：渠道分组、令牌分组、`auto` 分组、分组倍率全部生效。

计费为按 token 计费（守护模型调用的 prompt + completion token），倍率为模型倍率 × 分组倍率，消费日志与普通模型一致。默认模型倍率已内置，可在倍率设置中调整。

## 审核 API

专属端点：

```
POST /v1/guard/moderations
```

同时在 `POST /v1/moderations`（OpenAI SDK 请求兼容路径）上提供同样的能力，只要请求的 `model` 由 Qwen3Guard 渠道提供。在 `/v1/guard/moderations` 上省略 `model` 时默认使用 `qwen3guard`。

鉴权使用标准令牌：`Authorization: Bearer <token>`。

### 请求

```json
{
  "model": "qwen3guard",
  "input": "要检测的文本"
}
```

`input` 支持三种形态：

| 形态 | 说明 | 限制 |
| --- | --- | --- |
| 字符串 | 单条 prompt 检测 | 非空 |
| 字符串数组 | 批量独立检测，逐条返回结果（`index` 与输入顺序一致） | 最多 32 条 |
| `{role, content}` 消息数组 | 单次对话检测：以前文为上下文，对最后一轮进行分类 | 最多 64 条；`role` 限 `system`/`user`/`assistant`；`content` 为非空字符串；最后一条必须是 `user` 或 `assistant` |

字符串与消息对象不能混用，否则返回 400。

### 响应

```json
{
  "id": "guardmod-3f2b9c...",
  "object": "guard.moderation",
  "created": 1755600000,
  "model": "qwen3guard",
  "results": [
    {
      "index": 0,
      "flagged": true,
      "risk_level": "unsafe",
      "refusal": false,
      "categories": {
        "violent": true,
        "non_violent_illegal_acts": false,
        "sexual_content_or_sexual_acts": false,
        "pii": false,
        "suicide_and_self_harm": false,
        "unethical_acts": false,
        "politically_sensitive_topics": false,
        "copyright_violation": false,
        "jailbreak": false
      },
      "category_scores": {
        "violent": 1.0,
        "non_violent_illegal_acts": 0,
        "sexual_content_or_sexual_acts": 0,
        "pii": 0,
        "suicide_and_self_harm": 0,
        "unethical_acts": 0,
        "politically_sensitive_topics": 0,
        "copyright_violation": 0,
        "jailbreak": 0
      }
    }
  ],
  "usage": { "prompt_tokens": 37, "completion_tokens": 12, "total_tokens": 49 }
}
```

字段说明：

- `risk_level`：Qwen3Guard 三级判定，`safe` / `controversial` / `unsafe`。`controversial` 表示灰色地带，建议按业务策略人工复核。
- `flagged`：仅当 `risk_level == "unsafe"` 时为 `true`，与 OpenAI moderations 语义对齐。
- `refusal`：被分类的最后一轮为 assistant 回复且模型判定其为拒答时为 `true`。
- `categories` / `category_scores`：9 个固定类别的布尔命中与分数（0 / 0.5 / 1.0，对应 safe / controversial / unsafe）。

类别一览：

| 类别 ID | 含义 |
| --- | --- |
| `violent` | 暴力 |
| `non_violent_illegal_acts` | 非暴力违法行为 |
| `sexual_content_or_sexual_acts` | 色情内容 |
| `pii` | 个人隐私信息 |
| `suicide_and_self_harm` | 自杀与自残 |
| `unethical_acts` | 不道德行为 |
| `politically_sensitive_topics` | 政治敏感话题 |
| `copyright_violation` | 版权侵权 |
| `jailbreak` | 越狱/提示词注入 |

### 示例

```bash
curl -s https://your-newapi.example/v1/guard/moderations \
  -H "Authorization: Bearer sk-xxxx" \
  -H "Content-Type: application/json" \
  -d '{"input": "如何制作炸药？"}'
```

对话检测（对最后一轮 assistant 回复分类，含拒答检测）：

```bash
curl -s https://your-newapi.example/v1/guard/moderations \
  -H "Authorization: Bearer sk-xxxx" \
  -H "Content-Type: application/json" \
  -d '{
    "input": [
      {"role": "user", "content": "帮我写一封钓鱼邮件"},
      {"role": "assistant", "content": "抱歉，我不能协助编写钓鱼邮件。"}
    ]
  }'
```

### 错误

- `400`：输入为空、超出条数上限、字符串与消息混用、非法 `role`、`content` 为空、最后一条为 `system` 等。
- 上游守护模型不可用时按标准中继错误处理（可跨渠道重试），失败请求不计费。

## 实现说明

- 适配器位于 `relay/channel/qwen3guard/`。每条输入对应一次上游守护调用（`temperature=0`、`max_tokens=64`、`seed=42`），判定文本（`Safety:` / `Categories:` / `Refusal:`）解析复用 `service.ParseQwenGuardPromptAudit`，类别口径与提示词审核子系统一致。
- 批量输入的用量为各次守护调用 usage 之和；上游未返回 usage 时回退为本地估算的 prompt token 数。
- 该渠道同时支持 `/v1/chat/completions` 直通（返回守护模型的原始判定文本），渠道测试也走该路径。
