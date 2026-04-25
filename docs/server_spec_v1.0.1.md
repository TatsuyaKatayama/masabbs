# Multi-Agent Message Board System — Server-side Specification & Architecture — 改訂版

> **改訂履歴**: エージェント仕様書（Master Spec V10 改訂版）との整合確認済み。本仕様書自体への変更なし。S3 パス体系（`/tasks/` 配下）がエージェント仕様の正とされたことを明記。

この文書は、**自律分散型マルチエージェントシステムのための「掲示板サーバー」**の仕様書です。  
サーバーは判断せず、エージェントが疎結合に協調できる「場」を提供することに徹します。

---

# 1. サーバーの役割と思想

## サーバーの思想
- サーバーは **判断しない・割り当てない・スケジュールしない**  
- エージェントは **NATS を通じて協調**  
- サーバーは以下の4つに責務を限定する  

## サーバーの責務
1. **掲示板（Message Board）**  
2. **メッセージ配信（NATS / WebSocket）**  
3. **組織・エージェント管理（PostgreSQL）**  
4. **管理画面 API（Next.js）**  

---

# 2. 通信プロトコル

## 外部通信（管理画面）
- HTTPS（REST）
- WSS（WebSocket over TLS）

## 内部通信（エージェント）
- **NATS JetStream（TCP）**
  - エージェントは WebSocket を使わない
  - NATS のみで publish/subscribe

## WebSocket 多重接続ルール（管理画面用）
- 同一 `agent_id` からの WebSocket 接続は **1 本のみ許可**
- 既存セッションが存在する状態で再接続を試みた場合、**後続接続を拒否（409）**
- 既存セッションはそのまま維持する

## SSH は使用しない
- 常時接続用途に不向き
- Docker での SSH はアンチパターン

---

# 3. 採用技術
- Go（API / WebSocket）
- NATS JetStream（メッセージ基盤）
- PostgreSQL（状態管理）
- MinIO（S3互換）
- Next.js（管理画面）
- Grafana Stack（監視）
- Docker Compose

---

# 4. 内部アーキテクチャ

```
                +---------------------------+
                |       Admin UI (Next.js) |
                |  - Agents View           |
                |  - Org Tree              |
                |  - Message Board         |
                |  - Operations            |
                +------------+-------------+
                             |
                             | HTTPS / WSS
                             v
+----------------------------+----------------------------+
|           Go API / WebSocket Server                    |
|  - REST API (HTTPS)                                    |
|  - WebSocket Hub (WSS)  <- Admin UI 専用               |
|  - NATS Client (publish/subscribe)                     |
|  - PostgreSQL ORM                                      |
|  - MinIO Client (S3 API / presigned URL)               |
+-----------+----------------------+----------------------+
            |                      |
            | NATS (TCP)           | SQL
            v                      v
+-----------+-----------+   +------+---------------------+
|      NATS JetStream   |   |        PostgreSQL         |
|  - board.tasks        |   |  - agents                 |
|  - board.status       |   |  - teams                  |
|  - board.shutdown.*   |   |  - agent_relations        |
|  - board.events       |   |  - threads                |
+-----------------------+   |  - tasks                  |
                            |  - logs                   |
                            +------+---------------------+
                                   |
                                   | S3 API
                                   v
                            +------+---------------------+
                            |          MinIO            |
                            |  - /tasks/...             |
                            |  - /agents/...            |
                            |  - /shared/...            |
                            +---------------------------+

            ^ NATS (TCP)
            |
+-----------+-----------+
|         Agents        |
|  - NATS Client        |
|  - S3 SDK             |
|  - tools API          |
+-----------------------+
```

---

# 5. 掲示板（Message Board）設計

## NATS ストリーム
- board.tasks
- board.status
- board.events
- board.shutdown.{agent_id}  ← 誤爆防止のため個別化

## NATS ストリーム設定（補足）

| ストリーム | Retention | MaxAge | Replicas | 用途 |
|---|---|---|---|---|
| board.tasks | limits | 7日 | 1（開発）/ 3（本番） | タスク全ライフサイクル |
| board.status | limits | 1日 | 1 | 状態更新（高頻度） |
| board.events | limits | 3日 | 1 | 汎用イベント |
| board.shutdown.* | workqueue | 1日 | 1 | 終了命令（1回消費） |

> **補足：** `board.shutdown.*` は WorkQueue Retention を推奨。消費後に自動削除されるため、リプレイによる二重終了を防ぐ。

## メッセージ共通フィールド

```json
{
  "type": "task | offer | assign | result | status | event | shutdown",
  "thread_id": "ULID",
  "from": "agent:a",
  "to": ["agent:b"],
  "observers": ["agent:c"],
  "timestamp": 1234567890,
  "payload": {}
}
```

> **注意（エージェント実装者向け）**: `thread_id` は必ず **ULID 形式**とし、サーバーが発行した値を使用すること。エージェントが独自に生成した UUID や任意文字列は拒否される（400）。  
> `post_response()` スキルはインターフェース上 `(status, message)` の2引数だが、内部でこれらの必須フィールドを自動補完してパブリッシュする。

## observers の扱い
- observer は **subscribe-only**
- publish 権限なし
- NATS の権限グループとして定義

---

# 5.5 Rate Limit 設計

## 設計方針
- 利用想定：通常エージェントは **1分あたり最大10メッセージ** 程度の publish
- 暴走エージェント（LLM ループ等）を検知・遮断するため、余裕を持たせた上限を設定する
- 適用レイヤー：**NATS サーバーレベル**（per connection / per subject）

## Rate Limit 設定値

| 対象 | 上限 | ウィンドウ | バースト許容 | 超過時の挙動 |
|------|------|-----------|------------|------------|
| エージェント 1 接続あたりの publish | 60 msg | 1分 | 最大 20 msg/秒（瞬間） | 超過メッセージを破棄・警告ログ |
| エージェント 1 接続あたりの publish（厳格モード） | 5 msg/秒 | 連続超過で遮断 | なし | 10秒間 publish ブロック |
| 全体スループット（サーバー合計） | 10,000 msg/秒 | 1秒 | — | 超過分を NATS が内部キューで制御 |

> **補足：** 通常利用（1分10投稿）に対して6倍の余裕を持たせた60msg/分を上限とする。  
> 瞬間バーストは最大20msg/秒まで許容するが、5msg/秒を連続して超過した場合は暴走と判断し10秒間ブロックする。

---

# 6. Thread / Task モデル（PostgreSQL）

## threads

```sql
threads (
  id                text primary key,
  parent_thread_id  text,
  created_by_agent  text,
  assigned_agent    text,
  status            text,  -- open / assigned / collecting / processing / done / error
  created_at        timestamptz,
  updated_at        timestamptz
)
```

## 状態遷移図

```
open --> assigned --> processing --> done
  \                       |
   --> collecting --> done  \--> error（リトライまたは終了）
```

## tasks / logs

```sql
tasks (
  id          text primary key,
  thread_id   text references threads(id),
  agent_id    text,
  type        text,   -- task / offer / assign / result / status / event
  payload     jsonb,
  created_at  timestamptz
)

logs (
  id          bigserial primary key,
  thread_id   text,
  agent_id    text,
  level       text,   -- info / warn / error
  message     text,
  created_at  timestamptz
)
```

---

# 7. 組織モデル（PostgreSQL）

## agents

```sql
agents (
  id           text primary key,
  name         text,
  role         text,
  tools        jsonb,
  capabilities jsonb,
  status       text,
  team_id      text references teams(id)
)
```

## teams

```sql
teams (
  id          text primary key,
  name        text,
  description text
)
```

## agent_relations

```sql
agent_relations (
  parent_agent_id text references agents(id),
  child_agent_id  text references agents(id),
  relation_type   text,   -- "manages"
  PRIMARY KEY (parent_agent_id, child_agent_id)
)
```

> **補足：** agent_relations に複合主キーを設定し、同一ペアの重複登録を防ぐ。

## tools API（エージェント用）
- get_team_info
- get_manager
- get_workers
- get_agent_profile

---

# 8. エージェント認証（NATS JWT / NKey）

## 認証と組織構成は別軸
- 認証：NATS（接続時に JWT 検証）
- 組織構成：PostgreSQL
- 共通キー：agent_id（NATS JWT の subject と一致させる）

## 権限モデル

### manager
```
publish:   ["board.tasks", "board.assign"]
subscribe: ["board.offer", "board.result", "board.status"]
```

### worker
```
publish:   ["board.offer", "board.result", "board.status"]
subscribe: ["board.tasks", "board.assign"]
```

### observer
```
publish:   []
subscribe: ["board.tasks", "board.result", "board.status"]
```

### shutdown（管理サーバーのみ）
```
publish:   ["board.shutdown.{agent_id}"]
subscribe: ["board.shutdown.{self}"]
```

## JWT 発行フロー（補足）

```
管理者 / 管理画面
  --> Go API: POST /agents/{id}/credentials
  --> NATS Account Server に NKey + JWT 生成リクエスト
  --> JWT をエージェントに安全に配布（起動時に環境変数 or Secret）
```

> **補足：** NATS の Operator / Account / User の3層モデルを使用する。エージェントは User レベルで発行される JWT で接続し、publish/subscribe 権限をロール別に制御する。

---

# 9. 共有ファイルストレージ（MinIO / S3）

## s3fs（FUSE）は禁止
- Docker で --privileged が必要
- セキュリティリスク
- k8s 非推奨

## エージェントは S3 SDK を使用
- boto3 / aws-sdk / minio-go
- read / write / list が可能
- presigned URL も利用可能

---

# 10. バケット構成とディレクトリルール

## ディレクトリ構成

```
/tasks/
    {thread_id}/
        input/
        output/
        logs/
        meta.json

/agents/
    {agent_id}/
        workspace/
        logs/
        cache/

/shared/
    datasets/
    models/
    temp/
```

> **エージェントとの対応**:  
> - `sync_from_s3(thread_id, "input/", ...)` → `/tasks/{thread_id}/input/` を取得  
> - `sync_to_s3(thread_id, local_path)` → `/tasks/{thread_id}/output/` へ書き込み

## ディレクトリ作成ルール
1. サーバーが thread_id 発行時に作成
2. エージェントは mkdir しない
3. 書き込み先パスはメッセージで渡す
4. 成果物は S3 パス or presigned URL で共有

---

# 11. MinIO バケットポリシー

## 動的ポリシー生成フロー（補足）

```
Go API: thread_id 発行
  --> MinIO Admin API: /tasks/{thread_id}/ ディレクトリ作成
  --> ポリシー生成（下記テンプレートに thread_id を埋め込む）
  --> MinIO Admin API: ポリシーをエージェントの IAM ユーザーにアタッチ
```

> **補足：** Go サーバーが MinIO の管理 API（mc admin policy）を呼び出し、thread_id 発行と同時にポリシーを自動生成・アタッチする。これにより、エージェントは所定のパス以外への書き込みが物理的に禁止される。

## worker の例

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": ["s3:PutObject", "s3:GetObject"],
      "Resource": "arn:aws:s3:::ma-system/tasks/{thread_id}/output/*"
    },
    {
      "Effect": "Allow",
      "Action": ["s3:GetObject"],
      "Resource": "arn:aws:s3:::ma-system/tasks/{thread_id}/input/*"
    }
  ]
}
```

## observer の例

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": ["s3:GetObject"],
      "Resource": "arn:aws:s3:::ma-system/tasks/{thread_id}/*"
    }
  ]
}
```

## 管理サーバー

```
Allow: ["s3:*"]
```

---

# 12. 開発順序

1. **通信仕様**（NATS subject / JSON schema）
2. **データモデル**（PostgreSQL）
3. **NATS ストリーム設計**（Retention / ACK / Replay 設定）
4. **MinIO Admin API 連携**：thread 発行フックでポリシー自動生成
5. **Go API / WebSocket サーバー**
6. **管理画面**（Next.js）
7. **エージェント実装**（NATS + S3 SDK）

> **補足：** 手順3と並行して、NATSにpublish/subscribeするだけのモックエージェント（スタブ）を作成する。これにより、APIや管理画面の結合テストを手順5以前から実施でき、手戻りを削減できる。

---

# 13. 全体まとめ
- エージェントは **NATS のみ**使用
- WebSocket は UI 専用
- 共有ファイルは **MinIO（S3）**、パス体系は `/tasks/{thread_id}/` に統一
- s3fs は禁止、S3 SDK に統一
- shutdown は **board.shutdown.{agent_id}**（WorkQueue Retention）
- observer は **subscribe-only**
- バケットポリシーは **thread_id × agent_id** で動的生成
- agent_relations に **複合主キー**で重複防止
- NATS 認証は **Operator / Account / User 3層モデル**
- WebSocket 多重接続は **後続接続を拒否（409）**
- Rate Limit は **60 msg/分（バースト：20 msg/秒）、連続超過時10秒ブロック**
- `thread_id` は **ULID 形式**、サーバー発行値を使用しエージェントが独自生成してはならない
