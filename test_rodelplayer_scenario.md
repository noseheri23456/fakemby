# RodelPlayer 认证问题诊断

## 观察到的行为

1. RodelPlayer 获取 `/emby/Users/Public` → 获得用户列表（包含用户 ID）
2. RodelPlayer 重复发送：`GET /emby/Users/{userId}/Items` **没有任何认证信息**
3. 服务器返回 401
4. RodelPlayer 继续重复同样的请求（循环）

## 可能的解释

### 假设 1：RodelPlayer 期望"匿名访问"
- RodelPlayer 可能认为获取用户 ID 后，可以直接访问该用户的数据
- 某些 Emby 实现可能允许这种行为
- **解决方案**：实现一个 "guest token" 或允许公开用户访问

### 假设 2：RodelPlayer 有客户端认证方式
- RodelPlayer 可能使用特殊的 Header 或机制（如设备 ID + 设备密钥）
- 而不是用户名/密码 Token
- **解决方案**：检查 RodelPlayer 发送的其他 Header

### 假设 3：RodelPlayer 的 bug 或配置问题
- RodelPlayer 可能无法正确处理认证
- RodelPlayer 配置中可能缺少凭据
- **解决方案**：检查 RodelPlayer 设置，或使用其他客户端验证

## 下一步

1. **检查 RodelPlayer 发送的所有 Header**
   - 使用 Fiddler、Charles 或浏览器开发者工具
   - 拦截请求并查看完整的 HTTP 头
   
2. **尝试其他 Emby 客户端**
   - 官方 Emby 客户端
   - Infuse（Apple TV）
   - 其他开源客户端（Jellyfin 等）
   
3. **查看 RodelPlayer 源代码或文档**
   - RodelPlayer 期望什么样的认证？
   - 是否有特殊的配置选项？

4. **考虑"后退"方案**
   - 如果只是想让 RodelPlayer 工作，可以实现一个特殊的"简化"认证模式
   - 例如：允许从 `/emby/Users/Public` 获取的用户直接访问其数据（安全风险！）

## 当前状态

✅ 服务器端：认证系统完全正常
- 标准 Token 认证工作
- HTTP Basic Auth 已实现（但 RodelPlayer 可能不使用）
- 多种 Token 传递方式支持

❌ RodelPlayer：无法认证
- 不发送任何认证信息
- 不响应 401 错误
- 不尝试登录流程
