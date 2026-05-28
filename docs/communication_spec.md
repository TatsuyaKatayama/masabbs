# Communication Specification v1.0.1

このドキュメントは、masabbs における NATS サブジェクト階層、メッセージフォーマット、JetStream 設定、および接続制限を定義します。

## 1. NATS サブジェクト階層

サブジェクトは `board.<message_type>.<id>` の形式をとります。
`<id>` は `thread_id` または `agent_id` が入ります。

| メッセージ種別 | サブジェクト | ID の内容 | 用途 |
| :--- | :--- | :--- | :--- |
| **task** | `board.task.<thread_id>` | thread_id | 新規タスクの周知 |
| **offer** | `board.offer.<thread_id>` | thread_id | タスクへの立候補 |
| **assign** | `board.assign.<thread_id>` | thread_id | タスク実行者の決定 |
| **result** | `board.result.<thread_id>` | thread_id | 実行結果・成果物の報告 |
| **status** | `board.status.<agent_id>` | agent_id | エージェントの生存・進捗報告 |
| **event** | `board.event.<event_type>` | 任意 | システム全体の通知 |
| **shutdown** | `board.shutdown.<agent_id>` | agent_id | 特定エージェントへの終了命令 |

---

## 2. メッセージフォーマット (JSON)

全てのメッセージは以下の共通エンベロープを持ちます。

```json
{
  "type": "task | offer | assign | result | status | event | shutdown",
  "thread_id": "ULID",
  "from": "agent_id",
  "to": ["agent_id"],
  "observers": ["agent_id"],
  "timestamp": 1234567890,
  "payload": {}
}
```

### Payload 定義

| type | payload 必須フィールド | 説明 |
| :--- | :--- | :--- |
| **task** | `command`, `input_dir`, `deadline` | `input_dir` は S3 の相対パス |
| **offer** | `eta_seconds`, `confidence` | `confidence`: 0.0~1.0 |
| **assign** | `reason` (任意) | 決定理由 |
| **result** | `output_dir` (任意), `exit_code`, `message` (任意), `error` (任意) | `output_dir` は S3 の相対パス |
| **status** | `progress`, `state` | `state`: running/paused/error |
| **shutdown** | `reason` | 終了の理由 |

---

## 3. ストレージ (S3) 連携

- **パス形式**: 全て**相対パス**で指定します。
  - 例: `tasks/01J123ABC.../input/`
- **バケット名**: エージェント起動時に環境変数 `S3_BUCKET` として注入されます。
- **ディレクトリ構造**: 仕様書に従い、サーバーが thread 発行時に `input/`, `output/`, `logs/` を用意します。

---

## 4. NATS JetStream 設定

- **Stream Name**: `board_tasks`, `board_status`, `board_events`, `board_shutdown`
- **Retention Policy**:
  - `board_shutdown` は `WorkQueue` (消費後削除)
  - その他は `Limits`
- **AckPolicy**: `AckExplicit`
- **Durable Name**: `server-archiver-{stream_name}` (サーバー用), `{role}-{agent_id}` (エージェント用)

---

## 5. WebSocket 接続制限 (Admin UI 専用)

- **単一接続ルール**: 同一 `agent_id` からの WebSocket 接続は **1 本のみ**許可されます。
- **重複時の挙動**: 既にアクティブなセッションがある状態で再接続を試みた場合、サーバーは **HTTP 409 Conflict** を返し、後続の接続を拒否します。既存のセッションは維持されます。

---

## 6. Rate Limit (NATS サーバーレベル)

エージェントの暴走を防ぐため、以下の制限が適用されます。

| 対象 | 上限 | ウィンドウ | バースト許容 | 超過時の挙動 |
| :--- | :--- | :--- | :--- | :--- |
| **Publish (1接続あたり)** | 60 msg | 1分 | 20 msg/秒 | 破棄・警告 |
| **Publish (厳格モード)** | 5 msg/秒 | - | 連続超過で遮断 | 10秒間 publish ブロック |

---

## 7. ID (ULID) 生成ルール

1. **基本ルール**: サーバーが `POST /threads` エンドポイントで生成し、エージェントへ返却します。
2. **形式の厳格化**: 全ての `thread_id` は必ず **ULID 形式**でなければなりません。エージェントが独自に生成した ID は拒否されます。
3. **子タスク (派生)**: エージェントが子タスクを作成する場合も、サーバーの API を通じて `thread_id` を取得することを強く推奨します。
