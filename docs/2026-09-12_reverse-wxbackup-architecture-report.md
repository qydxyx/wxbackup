# 微备份（WxBackup）架构逆向报告

> 分析日期：2026-09-12  
> 分析人员：Grok（reverse-engineering skill）  
> 工具链：`gh`、`tar`/`gzip`、`file`、`strings`、`objdump`、公开 README  
> Flavor：null（普通逆向，非 malware/APT）  
> Case：`work/wxbackup-rebuild/`

## 1. 执行摘要

微备份是飞牛 fnOS 上的付费 NAS 应用：扫码把 NAS 登录成微信“电脑端”，再走官方局域网备份/恢复，最后用自建 SQLite + 搜索索引提供浏览器查看。GitHub 仓库本身只有 README 和截图；可执行逻辑在 Release 的 `.fpk` 里。

对 1.2.9.0 x86 FPK 的只读拆包表明：主程序是约 41MB 的剥离符号 Go ELF，前端是 Vite 打出来的 Vue/WeUI SPA（CGI 与直连端口两套），另附 ffmpeg、无头 Chromium 和 Go 写的 `index.cgi`。重建同类功能应走**适配器隔离的清洁室实现**，而不是移植该二进制里的微信协议栈或会员系统。

## 2. 范围与授权

见 `work/wxbackup-rebuild/scope.md`。

- auth：granted / own_system
- network：authorized_target_only（GitHub 公开仓库与 Release）
- in_scope：README、公开协议资料、同类 OSS、FPK 元数据与字符串级身份识别
- out_of_scope：会员破解、对微信生产环境攻击、从 ELF 复制协议实现

动态分析（真机备份抓包、Frida）本回合未做，因为没有用户设备授权会话。

## 3. 目标概述

| 属性 | 值 |
|------|---|
| 名称 | 微备份 WxBackup 1.2.9.0 x86 |
| 来源 | https://github.com/weibeifen/wxbackup |
| 文件 | `WxBackup_1.2.9.0_x86.fpk` |
| 大小 | 161MB（gzip tar） |
| SHA256 | `80b6b73d9ddaf2437da8ae9bc86f1ddd063192bc94224bc6fff5bdaa08285a93` |
| 类型 | fnOS FPK → `app.tgz` → Go ELF + Vite dist |
| 主二进制 SHA256 | `e0fddee0ae0a6dc947afead0655719613d0c356a574cd6e13f1b43ca479a8e2c` |

## 4. 分析目标

1. 这款产品实际怎样把手机微信数据搬到 NAS？
2. 运行时栈、端口、存储布局是什么？
3. 独立重建时应切哪些模块、避开哪些版权/ToS 面？

## 5. 静态分析

### 5.1 包结构

FPK 是标准飞牛包：`manifest`、`cmd/*` 生命周期、`config/privilege|resource`、`app.tgz`。

- `service_port=20365`，`checkport=true`
- 桌面入口 iframe → `/cgi/ThirdParty/WxBackup/index.cgi`
- `run-as: root`（绑定微信官方端口）
- 安装后把 `WxBackup`、`ffmpeg`、`chrome-linux.zip`、`dist`、`updater` 拷到 `/var/apps/WxBackup/var`
- `cmd/main` 把 Node.js 22 放进 PATH，但真正拉起的是 Go 二进制

### 5.1.1 导入表 / 等价锚点

`objdump -p WxBackup` 仅 `NEEDED libc.so.6`。Go 运行时与 sqlite cgo 静态进主文件。记为 E-003。

辅助二进制：`updater`、`ui/index.cgi` 为静态链接 Go。

### 5.2 关键逻辑（字符串级）

- 微信电脑协议：`WeChatProto.BackupStartRequest`、`BackupHeartBeatRequest`、`BakChatCreateQRCodePack`、`extdeviceloginconfirmok`
- 本地 API：`/api/wx/login|backup|restore*`、`/api/msg/*`、`/api/pwd/*`、`/api/products`
- 库表：`wx_chat`、`wx_friend`、`wx_backup_history`、`wx_stat`、`wx_yearly`、`dbs/global.db`
- 搜索：`search.index` + bleve
- 无头浏览器：CDP / `--no-sandbox` / `ms-playwright`，对应滑块 `swipvalid_err`

未做反编译，不还原算法伪代码。

### 5.3 加密

官方 `Backup.db` / `BAK_*` 在公开文献中为分片 + 会话密钥（SQLCipher / AES 类）。密钥来自电脑登录会话，不是 NAS 磁盘口令。访问密码只保护删除/查看，存在 `global.db`。

## 6. 动态分析

n/a。本回合无授权真机会话，未跑样本、未抓 LAN。行为结论全部来自 README 与静态字符串交叉验证。

## 7. Evidence 链

### 7.1 Evidence

| E-id | 标题 | 来源 | 哈希 |
|------|------|------|------|
| E-001 | README 产品合同 | GitHub | n/a |
| E-002 | FPK 清单与生命周期 | `samples/WxBackup_1.2.9.0_x86.fpk` | SHA256 见上 |
| E-003 | Go ELF + libc 导入 | `server/WxBackup` | 二进制 SHA256 见上 |
| E-004 | 前端 API 面 | Vite bundle | n/a |
| E-005 | SQLite / bleve 模式 | strings | n/a |
| E-006 | 电脑协议客户端特征 | strings | n/a |

### 7.2 Findings

| F-id | 结论 | evidence | confidence | status |
|------|------|----------|------------|--------|
| F-001 | Go 服务 + Vue/WeUI SPA + fnOS CGI | E-002 E-003 E-004 | high | validated |
| F-002 | 核心是电脑登录 + 官方 LAN 备份 | E-001 E-006 | high | validated |
| F-003 | 官方包与查看库双写 | E-001 E-005 | high | validated |
| F-004 | 增量按会话游标 | E-005 | medium | candidate |
| F-005 | 鸿蒙绑定 8011/24011，需特权端口 | E-001 E-002 | high | validated |
| F-006 | 会员/支付是独立控制面 | E-004 | medium | observed |

### 7.3 Path

P-001 `callflow`：CGI SPA → 扫码登录电脑态 → LAN 备份（8011/24011）→ 写 Backup.db/BAK_* → 整理进 wx_chat + search.index → `/api/msg` 查看；恢复走反向 LAN 或 Windows 微信读官方包。细节见 `work/wxbackup-rebuild/path.md`。

## 8. 核心发现

1. GitHub 仓库不是源码，是文档站；实现在 FPK。
2. 产品本质是 **NAS 上的微信电脑端备份接收器 + 自研查看器**。
3. 查看器合同完整（账号、备份作业、会话、搜索、媒体、设置），可独立重写。
4. 微信协议实现与会员系统必须留在适配器/商业边界之外。
5. 同类开源（greycodee/wechat-backup、nalzok/wechat-decipher-macos）已被 DMCA 下架；重建默认走官方客户端边车更稳。

## 9. 复现步骤

```bash
gh api repos/weibeifen/wxbackup/readme --jq .content | base64 -d | head
gh release download 1.2.9.0 --repo weibeifen/wxbackup --pattern '*x86.fpk'
shasum -a 256 WxBackup_1.2.9.0_x86.fpk
tar -tzf WxBackup_1.2.9.0_x86.fpk
tar -xzf WxBackup_1.2.9.0_x86.fpk manifest cmd
tar -xzf app.tgz server/WxBackup server/dist/cgi/index.html
file server/WxBackup
objdump -p server/WxBackup | grep NEEDED
strings -a server/WxBackup | grep -E 'WeChatProto.Backup|/api/wx/|CREATE TABLE IF NOT EXISTS `wx_'
```

## 10. 遗留问题

- 未在真机上验证 BackupStart 帧布局（需用户自己的手机+NAS 抓包）。
- `wx_backup_history.segment_meta` 编码未动态确认。
- 官方包 SQLCipher 参数需 Sidecar 路径上用自有备份文件验证。
- ARM FPK 未拆；manifest 分 arch，预期与 x86 同构。

## 11. 建议动作

完整重建步骤见 [`2026-09-12_wxbackup-rebuild-plan.md`](./2026-09-12_wxbackup-rebuild-plan.md)。

## 12. Timeline 摘要

| 时间 | 事件 |
|------|------|
| 01:37 | master-route → reverse-engineering |
| 01:38 | case-init `wxbackup-rebuild`，auth granted |
| 01:37–01:40 | README / Release / 同类 OSS |
| 01:40–01:44 | FPK 拆包、导入表、API、schema、协议字符串 |
| 01:45+ | 报告与重建计划 |

IOC：n/a（非恶意样本）。ATT&CK：n/a。
