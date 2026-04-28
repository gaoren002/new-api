# QQ 群邀请码注册接入

## 目标

让用户必须先进入 QQ 群，再通过 QQ 机器人私聊拿到一次性邀请码，最后才能完成 `new-api` 注册。

## 已支持的后端能力

当前仓库已经补了下面这套能力：

1. 开启 `邀请码注册` 后：
   - 密码注册需要邀请码
   - 微信首次注册需要邀请码
   - 首次 OAuth 注册需要邀请码
2. 提供机器人发码接口：
   - `POST /api/invite/bot/issue`
   - 同一 QQ 已有未过期、未使用邀请码时，接口直接返回已有邀请码，不重复生成新码
3. 机器人接口返回：
   - `code`
   - `expires_at`
   - `invite_link`

## 建议接法

推荐把 QQ 机器人和 `new-api` 解耦：

1. QQ 机器人负责：
   - 监听群消息
   - 识别触发词，例如 `获取邀请码`
   - 确认消息来自指定群
   - 调用 `new-api` 发码接口
   - 把返回的邀请码或注册链接私聊给用户
2. `new-api` 只负责：
   - 生成一次性邀请码
   - 注册时消费邀请码
   - 阻止未带邀请码的新用户注册

## 面板配置

在系统设置的登录注册区域：

1. 打开 `开启邀请码注册`
2. 设置 `邀请码有效期（分钟）`
3. 设置 `邀请码机器人密钥`

`邀请码机器人密钥` 留空时，机器人发码接口会被禁用。

## 机器人发码接口

请求：

```bash
curl -X POST 'https://your-domain/api/invite/bot/issue' \
  -H 'Authorization: Bearer your-invite-bot-secret' \
  -H 'Content-Type: application/json' \
  -d '{
    "qq_user_id": "123456789",
    "qq_group_id": "987654321",
    "source": "qq",
    "note": "group keyword: 获取邀请码"
  }'
```

返回：

```json
{
  "success": true,
  "message": "",
  "data": {
    "code": "A1B2C3D4",
    "expires_at": 1760000000,
    "invite_link": "https://your-domain/register?invite_code=A1B2C3D4",
    "qq_user_id": "123456789",
    "qq_group_id": "987654321",
    "source": "qq",
    "issued_at": 1759998200,
    "reusable_ttl": 30
  }
}
```

## 推荐机器人行为

群里收到：

```text
获取邀请码
```

机器人执行：

1. 判断 `group_id` 是否在白名单
2. 调用 `/api/invite/bot/issue`
   - 如果该 QQ 已有未过期、未使用的邀请码，接口会返回同一个邀请码，用于重复私聊发送
   - 如果该 QQ 已使用过邀请码，接口会拒绝再次签发
3. 私聊用户：

```text
你的邀请码：A1B2C3D4
注册链接：https://your-domain/register?invite_code=A1B2C3D4
邀请码 30 分钟内有效，只能使用一次。
```

4. 群里回复一句：

```text
邀请码已私聊，请查收。
```

## 建议限制

建议机器人侧再加一层限制：

1. 只响应指定群
2. 只响应精确关键词
3. 对同一 QQ 用户做冷却，例如 60 秒
4. 只私聊，不在群里公开邀请码
5. 优先发送 `invite_link`，减少用户手填错误

## 注意

这套方案只解决“进群后拿码再注册”。

是否“仍然留在群里”不在 `new-api` 当前职责内。如果你后面还想做“退群自动封号”，要在机器人侧继续监听退群事件，再调用你自己的封禁接口。
