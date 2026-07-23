# APIHub 浏览器伴侣

1. 在 APIHub 管理页打开“浏览器伴侣”，生成一次性配对码。
2. Chrome 打开 `chrome://extensions`，开启开发者模式并加载本目录。
3. 打开扩展弹窗，填写 APIHub 地址、设备名称和配对码。
4. 管理页选择站点并下发同源签到页 URL。

APIHub 地址必须使用 HTTPS。只有本地开发的 `localhost`、`127.0.0.0/8` 或 `[::1]` 地址允许使用 HTTP，设备令牌不会通过其他明文 HTTP 连接发送。

扩展每分钟检查一次任务，也可在弹窗点击“立即检查任务”。它不申请 `cookies`、`webRequest` 或 `scripting` 权限，不读取或上传 Cookie、LocalStorage、SessionStorage、Authorization、OAuth code、验证码或页面完整正文。弹窗可控制检测到登录或人机验证时是否前置窗口（默认开启）；十分钟未完成则任务标记为 `manual_required`。

活动任务和短期租约只保存在 Chrome 的会话级扩展存储中，用于 Manifest V3 service worker 重启后恢复；浏览器完全退出后不会保留。扩展会串行领取任务，同一设备同一时刻只执行一项。
