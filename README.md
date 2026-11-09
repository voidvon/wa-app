# wa-app

自托管 WhatsApp 账号管理与消息服务，提供 Dashboard、Proto gRPC 和同源 HTTP API。

## 架构与功能

- wa-app：单个 Go 二进制，同一进程提供 Dashboard、账号运行和账号添加服务。
- 账号添加：Android / iOS 协议注册、旧设备验证码转移、两步验证密码和账号交付。
- 账号运行：账号接纳、登录恢复、消息、联系人、群管理、账号设置及 Dashboard。
- 保留 wa-app-service、wa-account-add-service 两个独立入口及原有 Docker 部署。
- 长期数据使用 PostgreSQL 或 SQLite；单文件模式默认 SQLite，独立服务显式选择后端。短期缓存与连接租约默认使用内存，可选 Valkey。
- Proto 统一定义领域模型、RPC 和私有恢复状态；设备 SDK 能力通过独立 worker 接入。

Dashboard 首次访问必须创建管理员，之后使用用户名和密码登录；管理员凭据和登录会话保存在所选 SQL 数据库，密码仅保存 bcrypt 哈希。支持在管理页面配置飞书 Webhook，新消息解密后自动推送。

Dashboard 提供账号代理、重新登录、账号复核、邮箱验证、联系人与群资料管理、聊天分页、未读计数和消息回执。消息支持文本、商家模板正文、引用、编辑、撤回和反应；媒体消息展示类型与说明。

服务依赖、数据生命周期和代码导航见[架构说明](docs/architecture/README.md)。

## 单文件部署

构建需要 Go 1.27.2、Node.js 24、含标准 Proto 定义的 protoc 和两个 Go 生成器。构建后的主程序运行不需要 Docker、Go、Node.js、Python 或外置前端文件；主机需提供系统 CA 证书和可写数据目录。设备 SDK worker 按需单独接入。

~~~sh
go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.12
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.6.2
scripts/build-binary.sh
~~~

生成 `dist/wa-app`，包含与后端同时构建的 Dashboard。可以只复制这个可执行文件到同平台、同架构的目标主机。使用 `GOOS=linux GOARCH=arm64 scripts/build-binary.sh dist/wa-app-linux-arm64` 可交叉构建；Windows 输出文件使用 `.exe` 后缀。CI 同时生成 Linux / macOS 的 amd64、arm64 和 Windows amd64 制品，包含 SHA-256 校验和与 NOTICE。

首次启动直接运行：

~~~sh
./dist/wa-app
~~~

不带子命令时启动 Dashboard 和两个账号应用，默认在当前工作目录创建 `data/wa-app.sqlite3`，使用 SQLite 与内存短期状态。若已有账号数据库，设置 `WA_APP_SQLITE_FILE` 指向原文件；后台服务建议使用绝对路径。账号凭证、恢复状态和消息保存在外部数据库，升级时替换二进制并保留数据库与配置。

默认 Dashboard 为 <http://127.0.0.1:8080>，运行与添加 RPC 共用 `127.0.0.1:50091`。无需设置两服务间的 target。添加流程使用进程内存；退出后由 SQL 恢复已接纳账号，未完成添加流程重新开始。

`WA_SERVICE_TOKEN` 是 gRPC API 的共享访问凭证，与 WhatsApp 账号凭证和管理员登录独立。单文件模式未设置时，每次启动生成随机内部 token，仅保存在进程内存，不输出真实值。程序内部自动使用同一 token，直接使用 Dashboard 无需配置。外部 gRPC 客户端需要设置固定的 `WA_SERVICE_TOKEN`，并通过 `authorization: Bearer <token>` 调用。独立部署的两个服务仍需显式配置同一 token。

可选配置通过进程环境变量传入，程序不自动读取 `.env`；使用 `.env` 时由 shell 显式加载。

~~~sh
./dist/wa-app health
./dist/wa-app version
./dist/wa-app help
~~~

不带子命令时等同 `serve`。`health` 只需配置 gRPC 监听地址；`migrate` 只需数据库配置。已有旧结构数据库须先停写、备份，再按[迁移说明](docs/migration.md)执行 `./dist/wa-app migrate`，随后启动服务。

| 可选配置 | 用途 |
| --- | --- |
| WA_SERVICE_TOKEN | 未设置时自动生成；外部 gRPC 客户端使用固定值 |
| WA_APP_STORE | 默认 sqlite；使用 PostgreSQL 时显式设置 postgres |
| WA_APP_SQLITE_FILE | 默认 ./data/wa-app.sqlite3，相对于启动工作目录 |
| WA_APP_LISTEN_ADDR | 统一 gRPC 监听地址，默认 127.0.0.1:50091 |
| WA_APP_DASHBOARD_HTTP_ADDR | Dashboard 地址，默认 127.0.0.1:8080；空值关闭 HTTP |
| WA_APP_DASHBOARD_STATIC_DIR | 设置时从指定目录提供前端，默认使用内嵌资源 |
| WA_APP_PG_DSN | 显式选择 postgres 时连接外部 PostgreSQL；仅设置连接串会报错，不会回退到 SQLite |
| WA_APP_EPHEMERAL_STORE / WA_APP_VALKEY_URL | 显式选择 valkey 时连接外部 Valkey |
| WA_ACCOUNT_ADD_ANDROID_SIGNER_TARGET / WA_ACCOUNT_ADD_IOS_SIGNER_TARGET | 可选设备 worker，与原部署一致 |

需要对外提供 Dashboard 时，先在本机完成管理员设置，再设置监听地址，并由反向代理提供 HTTPS。可通过 systemd、launchd 等进程管理器设置工作目录、环境变量和自动重启。关闭进程会停止两个应用并清理短期状态。

直接开发可指定 `WA_APP_DASHBOARD_STATIC_DIR=./webui/dist go run ./cmd/wa-app`；完整内嵌构建使用上述构建脚本。原有独立服务仍可分别构建和启动。

## 管理员与飞书通知

1. 启动后打开 <http://127.0.0.1:8080>，首次进入管理员设置页。用户名为 3–64 字节且不含空格，密码为 12–72 字节，填写两次确认。
2. 创建后使用用户名和密码登录。未设置管理员或未登录时，账号、消息、添加流程等 HTTP 接口均拒绝访问。管理员保存在当前数据库，重启后直接登录。
3. 在侧栏点击“飞书消息通知”，在弹窗中填写群自定义机器人的 Webhook 地址；机器人开启签名校验时，同时填写签名密钥。勾选启用并保存，再点击“发送测试消息”。配置保存在数据库，立即生效，无需重启。
4. 新消息成功解密后，飞书会收到账号、会话、发送人、时间和消息正文；媒体推送文字摘要，不上传附件。历史导入、自己发送的消息、控制消息和回执不会触发通知。长正文会截断。机器人创建与安全设置见[飞书官方文档](https://open.feishu.cn/document/client-docs/bot-v3/add-custom-bot)，使用关键词校验时可设置关键词“WA”。

首次设置仅允许通过本机地址直接访问，禁止反向代理转发的初始化请求。远程服务器可建立 `ssh -L 18080:127.0.0.1:8080 user@your-server`，然后在浏览器打开 <http://127.0.0.1:18080/setup>；端口调整时相应修改命令。初始化结束后可正常通过公网域名登录。Compose 已为其仅发布到本机的 HTTP 端口启用 `WA_APP_SETUP_LOOPBACK_PROXY=true`，以适配 Docker 私网网关转发；该选项只适用于端口严格绑定宿主机 loopback 的本机转发。

登录状态默认保留 7 天。退出管理员登录会在服务端撤销当前会话。登录与设置操作有频率限制，所有修改请求均检查跨站来源；浏览器会话不再使用旧版共享密码。原 `WA_APP_AUTH_PASSWORD` 已移除，升级已有数据库也需先在本机创建管理员。`WA_SERVICE_TOKEN` 继续只负责 gRPC 访问认证。

飞书通知默认关闭。Webhook 与签名密钥属于敏感配置，保存后不会在页面、日志或账号 RPC 中回显；空白输入保留原值，可单独清除签名密钥。启用时接收的通知先写入 SQL 队列，再由后台发送；失败后一分钟重试，程序重启后继续。关闭通知暂停队列，重新开启会继续发送未送达消息；关闭期间新收消息不入队。按账号、会话、发送人与上游消息编号去重；网络响应丢失或进程在确认送达前退出时，可能重复提醒。替换 Webhook 后，待发送通知使用新配置。


## Docker 部署

需要 Git、Docker Compose v2 和 OpenSSL。已有数据库先按[迁移说明](docs/migration.md)完成停写、备份和离线转换。

~~~sh
git clone git@github.com:pood1e/wa-app.git
cd wa-app
cat > .env <<EOF
WA_SERVICE_TOKEN=$(openssl rand -hex 32)
WA_APP_STORE=sqlite
EOF
docker compose up -d --build --wait
~~~

Dashboard：<http://127.0.0.1:8080>。SQLite 数据保存在 wa-app-data 命名卷，容器内路径为 /var/lib/wa-app/wa-app.sqlite3。

~~~sh
docker compose ps
docker compose logs -f wa-app wa-account-add
docker compose down
~~~

两服务共享 WA_SERVICE_TOKEN。Compose 将 Dashboard 与运行服务 gRPC 绑定到本机，添加服务通过容器网络提供 gRPC。

## Docker 部署配置

配置写入根目录 .env，完整示例见 [.env.example](.env.example)。

| 变量 | 用途 |
| --- | --- |
| WA_SERVICE_TOKEN | 两服务共享认证，必填 |
| WA_APP_STORE | SQL 后端：sqlite 或 postgres，必填 |
| WA_APP_SQLITE_FILE | SQLite 文件路径 |
| WA_APP_PG_DSN / POSTGRES_PASSWORD | PostgreSQL 连接与 Compose 数据库密码 |
| WA_APP_EPHEMERAL_STORE | 短期状态：memory（默认）或 valkey |
| WA_APP_VALKEY_URL | Valkey 连接 |
| WA_COMMON_PROXY | 默认 HTTP CONNECT / SOCKS5 出站代理 |
| WA_ACCOUNT_ADD_TARGET / WA_ACCOUNT_RUNTIME_TARGET | 两服务 gRPC 地址，Compose 自动设置 |
| WA_ACCOUNT_ADD_ANDROID_SIGNER_TARGET / WA_ACCOUNT_ADD_IOS_SIGNER_TARGET | 设备 worker 地址 |
| WA_APP_IMAGE_TAG / WA_ACCOUNT_ADD_IMAGE_TAG | 两服务镜像标签，默认 latest |

PostgreSQL 部署设置 WA_APP_STORE=postgres 和 WA_APP_PG_DSN；使用 Compose 数据库时同时设置 POSTGRES_PASSWORD，连接主机为 postgres、用户和数据库为 wa_app。启动依赖后再启动两个服务：

~~~sh
docker compose --profile postgres up -d --wait postgres
docker compose --profile postgres up -d --build --wait wa-app wa-account-add
~~~

Valkey 部署设置 WA_APP_EPHEMERAL_STORE=valkey 与 WA_APP_VALKEY_URL=valkey://valkey:6379/0，然后启动对应 profile：

~~~sh
docker compose --profile valkey up -d --wait valkey
docker compose up -d --build --wait wa-app wa-account-add
~~~

账号级代理可在添加页指定或在详情页修改，优先于 WA_COMMON_PROXY；未配置代理时直连。代理凭证保存在 SQL，公开读取时脱敏。

设备 worker 的构建与连接见 [Android 签名服务](tools/android-attestation/README.md)和 [iOS App Attest worker](tools/ios-attestation/README.md)。目标需能从添加服务容器访问，证明能力按配置与设备可用性启用，上游响应决定账号流程结果。

### 发布镜像

| 服务 | 镜像 |
| --- | --- |
| 账号运行 | ghcr.io/pood1e/wa-app/wa-app-service |
| 账号添加 | ghcr.io/pood1e/wa-app/wa-account-add-service |

镜像支持 linux/amd64 与 linux/arm64。main 发布 latest 和 sha-<提交短哈希>，v* 标签发布对应版本。

~~~sh
docker compose pull wa-app wa-account-add
docker compose up -d --no-build --wait wa-app wa-account-add
~~~

## 开发与检查

需要 Go 1.27.2、Node.js 24 和含标准 Proto 定义的 protoc。Debian / Ubuntu 使用 protobuf-compiler libprotobuf-dev。

~~~sh
go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.12
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.6.2
scripts/generate-proto.sh
npm --prefix webui ci
npm --prefix webui run proto
scripts/lint-go.sh
go test -race ./cmd/... ./internal/...
npm --prefix webui test
npm --prefix webui run lint
npm --prefix webui run build
~~~

WA_TEST_PG_DSN 与 WA_TEST_VALKEY_URL 用于真实后端集成测试，数据库测试使用独立临时 schema。[CI](.github/workflows/test.yml)执行同一组检查。

| 入口 | 内容 |
| --- | --- |
| [架构说明](docs/architecture/README.md) | 服务依赖、数据生命周期和代码导航 |
| [数据迁移](docs/migration.md) | 停写、备份、转换、切换与恢复 |
| [Proto 契约](proto/byte/v/forge/waapp) | 公共 API 与私有协议状态 |
| [iOS 注册联调](cmd/wa-ios-onboarding/README.md) | 命令行输入和流程控制 |
