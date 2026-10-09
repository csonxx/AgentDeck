# AgentDeck Flutter 客户端

原生 Android / iOS / macOS 客户端，生产网络使用 dart:io，不支持 Flutter Web。

直连扫码，或选择“通过网关 · 飞书登录”，经 S-GATEWAY 发现同账号工作站，首次核对双方校验码。会话、思考摘要、工具块、Markdown 和图片均由客户端解密展示；安全存储与缓存加密见上级 [安全说明](../docs/SECURITY.md)。

```sh
flutter pub get
flutter analyze
flutter test
flutter build apk --release --split-per-abi
flutter build ios --release --no-codesign
```

跨语言/三角色网络测试通过项目根 `scripts/verify.sh` 启动合成 fixture。测试不连接真实飞书、不调用计费模型；没有 native/pubic-domain 环境的测试不能代表生产扫码验收。完整配置见 [网关指南](../docs/GATEWAY.md)。
