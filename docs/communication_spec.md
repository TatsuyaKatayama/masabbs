# MASABBS Communication Specification — 現行仕様

この文書は MASABBS の NATS subject、message envelope、REST 経由の thread 作成、Admin UI WebSocket の通信仕様を定義する。

---

## 1. 通信経路

| 経路 | 用途 |
|---|---|
| REST API | Admin UI からの agent / team / thread / backup 操作 |
| WebSocket | Admin UI へのリアルタイム更新 |
| NATS JetStream | agent 間の task / result / offer / assign 等の非同期通信 |
| MinIO S3 API | thread artifacts の入出力 |

---

## 2. NATS subject

サブジェクトは `board.<type>.<id>` を基本形とする。

| type | subject | id | 用途 |
|---|---|---|---|
| task | `board.task.<thread_id>` | thread_id | 新規 task 通知 |
| offer | `board.offer.<thread_id>` | thread_id | task への立候補 |
| assign | `board.assign.<thread_id>` | thread_id | 実行者決定 |
| result | `board.result.<thread_id>` | thread_id | 実行結果 |
| event | `board.event.<event_type>` | event_type | システム通知 |

---

## 3. Message envelope

```json
{
  "id": "optional-message-id",
  "type": "task | offer | assign | result | event",
  "thread_id": "thread-id",
  "from": "agent-id",
  "to": ["agent-id"],
  "observers": ["agent-id"],
  "timestamp": 1234567890,
  "payload": {}
}
```

### フィールド

| Field | Required | 説明 |
|---|---:|---|
| `id` | no | message id。DB の `tasks.id` に対応 |
| `type` | yes | message type |
| `thread_id` | no | thread 紐付け。event では省略可能 |
| `from` | yes | 送信 agent id |
| `to` | no | 宛先 agent ids |
| `observers` | no | 監視 agent ids |
| `timestamp` | yes | Unix 秒 |
| `payload` | yes | type ごとの body |

---

## 4. Payload

| type | payload | 説明 |
|---|---|---|
| task | `command`, `input_dir`, `deadline` | task 指示 |
| offer | `eta_seconds`, `confidence` | 立候補情報 |
| assign | `reason` | assign 理由 |
| result | `output_dir`, `exit_code`, `message`, `error` | 実行結果 |
| event | 任意 JSON | システムイベント |

`output_dir` は MinIO の相対 prefix を想定する。Admin UI は `/api/v1/storage/files` と `/api/v1/storage/presign` を通じて artifacts を表示する。

---

## 5. REST からの thread 作成

Admin UI は `POST /api/v1/threads` で thread を作成する。Message Board では選択中 team を `team_id` として送信できる。

```json
{
  "thread_id": "optional-existing-thread-id",
  "command": "調査して結果を返して",
  "created_by_agent": "admin-ui",
  "to": ["agent-1", "agent-2"],
  "observers": ["observer-1"],
  "parent_thread_id": "optional-parent-thread-id",
  "deadline": "2026-06-07T12:00:00Z",
  "team_id": "optional-team-id"
}
```

### 挙動

- `thread_id` 未指定時はサーバーが ULID を発行する
- `team_id` 指定時は `threads.team_id` に保存する
- `tasks.to_agents` と `tasks.observers` に宛先情報を保存する
- thread 作成時に MinIO 上の `tasks/{thread_id}/...` prefix を作る
- 作成された task は NATS に publish される

---

## 6. Team と agent の関係

agent 登録と team 所属は分離されている。

- `POST /api/v1/agents` は agent のみ作成する
- team 所属は `POST /api/v1/teams/:id/agents/:agent_id` で追加する
- agent は複数 team に所属できる
- relation は team 内の所属 agent 同士にのみ作成できる

---

## 7. WebSocket

WebSocket は Admin UI 用である。

接続後、Admin UI は以下の HTTP fetch で初期状態を取得する。

- `GET /api/v1/agents`
- `GET /api/v1/threads`
- `GET /api/v1/tasks`

その後、WebSocket message により画面上の message / thread / agent state を更新する。

---

## 8. Backup / Restore payload

### ConfigurationSnapshot

```json
{
  "teams": [],
  "agents": [],
  "team_agents": [],
  "relations": []
}
```

対象 API:

- `GET /api/v1/configs/export`
- `POST /api/v1/configs/import`
- `POST /api/v1/configs/:id/load`

restore は replace。threads/tasks/logs は削除される。

### ThreadSnapshot

```json
{
  "threads": [],
  "tasks": [],
  "logs": []
}
```

対象 API:

- `GET /api/v1/threads/export`
- `POST /api/v1/threads/import`

restore は replace。teams/agents は既存である必要がある。

### FullSnapshot

```json
{
  "teams": [],
  "agents": [],
  "team_agents": [],
  "relations": [],
  "threads": [],
  "tasks": [],
  "logs": [],
  "configs": []
}
```

`configs` は保存済み preset のバックアップであり、active state ではない。active agent の mission/name/role/status を手編集して復元する場合は top-level `agents` を変更する。

対象 API:

- `GET /api/v1/snapshot/export`
- `POST /api/v1/snapshot/import`

restore は transaction 内で replace し、失敗時は rollback する。

---

## 9. MinIO path

thread ごとに以下を使う。

```text
tasks/{thread_id}/input/
tasks/{thread_id}/output/
tasks/{thread_id}/logs/
tasks/{thread_id}/meta.json
```

message payload では相対 path/prefix を使う。

---

## 10. 実装上の注意

- team 所属の正規データは `team_agents`
- `agents.team_id` は互換用
- Message Board の team filter は `threads.team_id` を使う
- backup/restore は Settings 画面で扱う
- restore はすべて replace であり merge ではない

---

## 11. 改善通信仕様（V3 / Step 0〜7 拡張）

本セクションは、2026-06-09 改善案（Step 0〜7）に基づいて新しく拡張された通信経路、REST API エンドポイント、およびデータ構造を定義する。

### 11.1 新規追加・拡張された REST API 一覧

| Method | Path | 説明 |
|---|---|---|
| POST | `/api/v1/threads` | メンション必須のスレッド・サブスレッド作成。作成権限チェック（TeamManager/Chef）を適用 |
| POST | `/api/v1/threads/:id/messages` | 本文のメンションから `to_agents` を自動解決し、NATS配信代行およびDB保存を行う |
| POST | `/api/v1/threads/:id/reflection-requests` | 振り返り専用サブスレッドの起立要求を NATS にパブリッシュ |
| POST | `/api/v1/reflections` | チーム内エージェントへの相互評価の登録・更新（Upsert形式） |
| GET | `/api/v1/threads/:id/kpi` | 指定スレッド（再帰サブスレッド階層）の活動指標と D3.js 用ネットワークデータを取得 |
| GET | `/api/v1/teams/:id/kpi` | 指定チーム内の全活動指標と D3.js 用ネットワークデータを取得 |

### 11.2 NATS 配信代行仕様（`post_message` エンドポイント）
*   エージェントが `POST /api/v1/threads/:id/messages` を叩いた際、サーバーは本文（`message`）から自動的にメンション（`@agent-id`, `@team`）をパブリッシャーとして解決し、NATS の **`board.result.<thread_id>`** トピック宛てに従来の `MessageEnvelope` の形式で代理パブリッシュを行います。

### 11.3 振り返り（Reflection）メッセージング仕様
*   `POST /api/v1/threads/:id/reflection-requests` 時に自動起立する振り返り用サブスレッド（`reflection_thread_id`）について、サーバーは NATS の **`board.task.<reflection_thread_id>`** トピック宛てに、メンバー全員を `To` 配列にアサインした `task` ペイロードメッセージをパブリッシュします。
*   各エージェントは通常の仕事と同様に `check_board()` などのポーリング機構により、この振り返りタスクを安全に取得可能です。

### 11.4 KPI / D3.js ネットワーク JSON ペイロード構造
`GET /api/v1/threads/:id/kpi` および `GET /api/v1/teams/:id/kpi` で返却される `network_data` は、以下の D3.js 適合形式となっており、Admin UI 側でそのまま力学指向グラフのレンダリングに使用されます。

```json
{
  "network_data": {
    "nodes": [
      { "id": "agent-1", "count": 12 },
      { "id": "agent-2", "count": 8 }
    ],
    "links": [
      { "source": "agent-1", "target": "agent-2", "value": 5 }
    ]
  }
}
```
*   `nodes[].count`: 各エージェントが送信した総メッセージ数（円の大きさにマッピング）
*   `links[].value`: 該当する発信者から受信者へのメンション回数の総数（矢印線の太さにマッピング）

