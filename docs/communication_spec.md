# Communication Specification v1.0.0

このドキュメントは、masabbs における NATS サブジェクト階層、メッセージフォーマット、および JetStream の動作設定を定義します。

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
  "thread_id": "ULID (optional)",
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
| **result** | `output_dir`, `exit_code`, `error` (任意) | `output_dir` は S3 の相対パス |
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

- **Stream Name**: `board` (Subject: `board.>`)
- **Retention Policy**:
  - `board.shutdown.*` は `WorkQueue` (消費後削除)
  - その他は `Limits`
- **AckPolicy**: `AckExplicit`
  - メッセージの確実な処理を保証するため、エージェントは処理完了後に必ず ACK を返す必要があります。
- **Durable Name**: `{role}-{agent_id}`
  - 例: `worker-agent_b`
  - エージェント再接続時に、未処理のメッセージをここから再開します。

---

## 5. ID (ULID) 生成ルール

1. **基本ルール**: サーバーが `POST /threads` エンドポイントで生成し、エージェントへ返却します。
2. **子タスク (派生)**: エージェントが自律的に生成して良い。その際、必ず `parent_thread_id` を付与して NATS へ publish します。
3. **衝突回避**: ULID の特性および `parent_thread_id` による階層化により、分散環境での衝突リスクを許容範囲内として扱います。
