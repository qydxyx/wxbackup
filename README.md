# wxbackup

> 飞牛 NAS（fnOS）及家庭私有云微信备份、查看与恢复工具（开源净室重构版）

`wxbackup` 是一款专为家庭 NAS 及私有云环境打造的微信备份管理工具。支持多账号隔离、访问密码保护、聊天记录全文检索、富媒体查看、官方格式备份包解析与还原导出。所有聊天数据与多媒体文件完全保存在用户本地设备上，不依赖任何外部云端服务，守护个人隐私与数据安全。

---

## 🌟 核心特性

- 🔒 **数据自主可控与隐私安全**
  - 完全运行在本地 NAS 或局域网私有环境，数据零外发。
  - 支持多微信账号独立管理，每个账号可单独配置存储路径并设置独立的访问密码保护。
- 💬 **高拟真 Web 聊天查看器（Vue 3 + WeUI 风格）**
  - 原生支持 18+ 种微信常用消息类型精细化渲染：
    - **基础通讯**：文本（支持高亮与换行）、高保真图片预览、语音条播放、视频播放、位置地图展示。
    - **社交表情**：Emoji 表情符号、GIF 动图、拍一拍与系统提示。
    - **应用与互动**：链接分享卡片、小程序卡片、微信名片、文件附件。
    - **财务交易**：微信红包、转账状态及金额展示。
    - **多层级消息**：引用回复、合并转发记录（支持多层级嵌套展开与查看）。
    - **社群与互动**：群公告、群接龙、视频号、视频号直播、礼物等。
- 🔍 **毫秒级全文搜索（SQLite FTS5）**
  - 内置规范化 SQLite 数据库与 FTS5 全文索引。
  - 支持海量历史聊天记录按关键词进行毫秒级分词搜索，快速定位历史会话。
- 📦 **官方格式兼容与还原导出**
  - 内置微信官方电脑端备份格式编解码器（`backupfmt`）。
  - 支持将规范化数据库导出为官方微信备份包格式，便于后续在需要时迁移或还原。
  - 提供多维度统计看板（消息量、会话数、媒体资源占用等）。
- 🔌 **灵活的双协议适配机制（DeviceSession）**
  - **Sidecar 旁路目录监听适配器**：与官方电脑端微信备份目录（Windows / macOS）协同工作，静默监听 `Backup.db` 与 `BAK_*` 分片并自动摄取入库。
  - **Capture 网络协议适配器骨架**：预留局域网电脑端备份端口协议扩展接口（遵循安全与授权隔离原则）。
- 🚀 **多元化打包部署**
  - **飞牛 fnOS 原生应用包（FPK）**：集成应用桌面入口（CGI 反向代理）、权限控制规范与卸载数据清理向导。
  - **Docker / Docker Compose**：多阶段轻量构建镜像，支持 host 网络与自定义卷持久化挂载。

---

## 📁 目录架构说明

| 目录 / 文件 | 说明 |
| :--- | :--- |
| `cmd/server/` | Go 后端服务入口（HTTP API、SPA 静态资源托管、服务路由） |
| `internal/app/` | 核心应用逻辑（备份状态机、还原状态机、访问密码、数据统计） |
| `internal/domain/` | 领域模型与消息类型规范（`msgtype` 渲染目录定义、端口常量等） |
| `internal/store/` | SQLite 规范数据库与 FTS5 全文索引实现 |
| `internal/adapter/` | 官方格式编解码器（`backupfmt`）、媒体处理管道与 Sidecar/Capture 适配器 |
| `web/` | 基于 Vue 3 + Vite 的 Web 聊天记录查看器前端 |
| `deploy/fnos/` | 飞牛 fnOS 应用包构建目录（`manifest`, `config`, `cmd`, `wizard`） |
| `deploy/docker/` | Dockerfile 与 docker-compose.yml 部署配置 |
| `work/` | 净室逆向工程架构分析报告、重构路线图与元数据记录 |

---

## 🚀 快速开始

### 方式一：Docker 部署（推荐）

推荐使用 `docker-compose` 部署在 NAS 或 Linux 服务器上：

```bash
# 从仓库根目录启动容器（使用 host 网络以支持局域网协议发现）
docker compose -f deploy/docker/docker-compose.yml up -d --build
```

或者使用 `docker run`：

```bash
docker build -f deploy/docker/Dockerfile -t wxbackup .

docker run -d \
  --name wxbackup \
  --restart unless-stopped \
  --network host \
  -e WXBACKUP_PORT=20365 \
  -e WXBACKUP_DATA=/data \
  -v "$(pwd)/deploy/docker/data:/data" \
  wxbackup
```

服务启动后，在浏览器中访问：`http://<NAS_IP>:20365`，健康检查接口为 `http://<NAS_IP>:20365/health`。

---

### 方式二：飞牛 fnOS 应用安装（FPK）

针对飞牛 NAS 用户，支持直接打包为 `.fpk` 原生应用包：

1. **编译与打包**：
   ```bash
   # 编译 Linux amd64 二进制文件
   mkdir -p deploy/fnos/app/bin
   GOOS=linux GOARCH=amd64 CGO_ENABLED=1 go build -o deploy/fnos/app/bin/wxbackup ./cmd/server

   # 使用 fnpack 打包
   chmod +x deploy/fnos/cmd/* deploy/fnos/app/ui/index.cgi
   fnpack build --directory deploy/fnos
   ```

2. **安装**：
   在飞牛桌面打开 **应用中心 → 手动安装**，上传生成的 `.fpk` 文件完成安装。

---

### 方式三：本地源码运行与开发调试

#### 1. 前端构建
```bash
cd web
npm install
npm run build
cd ..
```
*构建产物将输出到 `web/dist`，Go 后端服务启动时会自动检测并挂载该目录作为 SPA 静态前端。*

#### 2. 后端启动
```bash
# 设置数据存储目录和监听端口
export WXBACKUP_PORT=20365
export WXBACKUP_DATA=./data

go run ./cmd/server
```

---

## ⚙️ 环境变量配置

| 变量名 | 默认值 | 说明 |
| :--- | :--- | :--- |
| `WXBACKUP_PORT` | `20365` | Web 服务监听端口 |
| `WXBACKUP_DATA` | `./data` | 数据库与媒体文件持久化存储目录 |
| `WXBACKUP_SIDECAR_DIR` | *(未设置)* | 监听的官方微信电脑端备份导出目录（可选） |
| `WXBACKUP_WEB` | *(自动探测)* | 自定义 Web 前端静态构建资源路径（可选） |

---

## 💡 微信备份协同工作模式（Sidecar 模式）

由于本工具不侵入、不修改微信客户端，目前最稳定便捷的备份摄取方式为 **Sidecar 旁路目录监听模式**：

1. **在官方微信电脑端触发备份**：
   - 打开电脑端微信（Windows 或 macOS），进入「设置 / 迁移与备份 → 备份与恢复 → 备份到电脑」。
   - 官方客户端会在本地生成标准备份目录：
     ```text
     Backup.db
     BAK_0_TEXT  BAK_0_MEDIA
     BAK_1_TEXT  BAK_1_MEDIA
     ...
     ```
2. **挂载或同步至本服务**：
   - 将上述目录配置为 `WXBACKUP_SIDECAR_DIR`（或挂载为 Docker 的 sidecar 卷）。
3. **自动摄取与浏览**：
   - 服务检测到文件写入完成并解密/解码后，自动转换为内部规范 SQLite 数据库，即可在 Web 端实时查阅、搜索历史记录。

---

## 📡 接口与能力（REST API）

后端提供完整的标准化 JSON REST API：

- **账号与安全**：
  - `GET /v1/accounts`：获取已摄取账号列表
  - `PUT /v1/accounts/{id}/settings`：修改账号备份路径
  - `PUT /v1/accounts/{id}/password`：设置/更新访问密码
  - `POST /v1/accounts/{id}/password/verify`：访问密码验证
  - `DELETE /v1/accounts/{id}`：删除账号及数据
- **会话与消息**：
  - `GET /v1/accounts/{id}/conversations`：获取会话列表
  - `GET /v1/accounts/{id}/conversations/{talker}/messages`：游标分页获取聊天记录
  - `GET /v1/accounts/{id}/search?q={keyword}`：FTS5 全文搜索
  - `GET /v1/media/{hash}`：多媒体资源流式获取
- **作业调度与状态机**：
  - `POST /v1/accounts/{id}/backup`：触发备份任务
  - `GET /v1/accounts/{id}/backup/status`：查询备份进度
  - `POST /v1/accounts/{id}/restore`：创建还原/导出任务
  - `GET /v1/accounts/{id}/stats`：账号数据统计

---

## 🛡️ 合规声明

- 本项目采用**净室反向工程与自主重构**（Clean-room reverse engineering）规范开发，代码库不包含任何闭源专有代码与商用密钥。
- 本项目仅供学习、研究以及个人多媒体数据自主容灾备份之用。请在遵循相关法律法规与平台服务条款的前提下使用。

---

## 📄 开源许可证

本项目基于 MIT License 开源。
