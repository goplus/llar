# LLAR API 设计

## 概述

LLAR API 分为两层：
- **用户 API（User API）**：面向 LLAR Cli，提供制品查询、构建请求提交等功能
- **数据管理 API（Data Management API）**：内网 API，供构建集群使用，处理制品上传和构建任务状态更新

## 1. 用户 API（User API）

### 1.1 查询制品

查询指定包的预构建制品是否存在。

**请求**
```
GET /api/v1/artifacts
```

**查询参数**
| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| package_id | string | 是 | 包的 UUID-4 标识符 |
| version | string | 是 | 包版本号 |
| platform | string | 是 | 目标平台（如 linux、darwin、windows） |
| arch | string | 是 | 目标架构（如 amd64、arm64） |
| config | string | 否 | 额外的构建配置哈希 |

**响应**
```json
{
  "found": true,
  "artifact": {
    "id": "artifact-uuid",
    "package_id": "package-uuid",
    "version": "1.0.0",
    "platform": "linux",
    "arch": "amd64",
    "download_url": "https://storage.example.com/artifacts/...",
    "checksum": "sha256:...",
    "size": 1048576,
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

### 1.2 提交构建请求

当制品不存在时，提交构建任务到消息队列。

**请求**
```
POST /api/v1/build-requests
```

**请求体**
```json
{
  "package_id": "package-uuid",
  "package_name": "DaveGamble/cJSON"
}
```

**响应**
```json
{
  "request_id": "request-uuid",
  "status": "queued",
  "created_at": "2024-01-01T00:00:00Z",
  "estimated_wait_time": 300
}
```

**说明**
- `package_id`: 包的唯一标识符
- `package_name`: 包名称，用于从配方仓库获取构建配方
- 未来将支持第三方配方仓库，通过 package_name 的前缀或配置来识别配方来源

### 1.3 查询构建请求状态

查询已提交构建请求的当前状态。

**请求**
```
GET /api/v1/build-requests/{request_id}
```

**响应**
```json
{
  "request_id": "request-uuid",
  "status": "building",
  "package_id": "package-uuid",
  "version": "1.0.0",
  "platform": "linux",
  "arch": "amd64",
  "created_at": "2024-01-01T00:00:00Z",
  "started_at": "2024-01-01T00:01:00Z",
  "node_id": "build-node-2",
  "progress": 45
}
```

**状态值**
- `queued`: 已加入队列，等待构建
- `building`: 正在构建中
- `completed`: 构建完成
- `failed`: 构建失败

### 1.4 获取包信息

获取包的基本信息和可用版本。

**请求**
```
GET /api/v1/packages/{package_id}
```

**响应**
```json
{
  "package_id": "package-uuid",
  "name": "DaveGamble/cJSON",
  "description": "Ultralightweight JSON parser in ANSI C",
  "homepage": "https://github.com/DaveGamble/cJSON",
  "versions": [
    {
      "version": "1.7.18",
      "released_at": "2024-01-01T00:00:00Z",
      "available_platforms": ["linux", "darwin", "windows"]
    }
  ]
}
```

## 2. 数据管理 API（Data Management API）

### 2.1 上传构建制品

构建节点完成构建后，上传制品到云存储。

**请求**
```
POST /api/internal/v1/artifacts
```

**请求头**
```
Authorization: Bearer <internal-token>
```

**请求体（multipart/form-data）**
```
package_id: package-uuid
version: 1.0.0
platform: linux
arch: amd64
config: optional-config-hash
file: <binary-file>
checksum: sha256:...
```

**响应**
```json
{
  "artifact_id": "artifact-uuid",
  "download_url": "https://storage.example.com/artifacts/...",
  "stored_at": "2024-01-01T00:00:00Z"
}
```

### 2.2 更新构建请求状态

构建节点更新构建任务的状态。

**请求**
```
PATCH /api/internal/v1/build-requests/{request_id}
```

**请求头**
```
Authorization: Bearer <internal-token>
```

**请求体**
```json
{
  "status": "building",
  "node_id": "build-node-2",
  "progress": 45,
  "logs_url": "https://logs.example.com/...",
  "error_message": null
}
```

**响应**
```json
{
  "request_id": "request-uuid",
  "updated_at": "2024-01-01T00:00:00Z"
}
```

### 2.3 获取配方

构建节点从中心化配方仓库获取配方。

**请求**
```
GET /api/internal/v1/formulas/{package_id}
```

**查询参数**
| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| version | string | 否 | 指定版本，不指定则返回最新版本 |

**响应**
```json
{
  "package_id": "package-uuid",
  "version": "1.0.0",
  "formula": {
    "source_url": "https://github.com/DaveGamble/cJSON/archive/v1.7.18.tar.gz",
    "source_checksum": "sha256:...",
    "build_script": "base64-encoded-xgo-classfile",
    "dependencies": []
  }
}
```

## 3. 认证与授权

### 用户 API
- 使用 API Key 认证
- 请求头：`X-API-Key: <user-api-key>`

### 数据管理 API
- 使用内网 Token 认证
- 请求头：`Authorization: Bearer <internal-token>`
- 仅允许内网 IP 访问

## 4. 错误响应

所有 API 错误响应格式统一：

```json
{
  "error": {
    "code": "ARTIFACT_NOT_FOUND",
    "message": "The requested artifact does not exist",
    "details": {}
  }
}
```

**常见错误码**
- `ARTIFACT_NOT_FOUND`: 制品不存在
- `PACKAGE_NOT_FOUND`: 包不存在
- `INVALID_REQUEST`: 请求参数无效
- `BUILD_FAILED`: 构建失败
- `UNAUTHORIZED`: 认证失败
- `RATE_LIMIT_EXCEEDED`: 超过请求频率限制
