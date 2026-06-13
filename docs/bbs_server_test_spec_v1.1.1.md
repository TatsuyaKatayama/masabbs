# MASABBS Test Specification — 現行仕様

この文書は、現行 MASABBS の Go API / PostgreSQL / Admin UI / backup-restore 仕様に対するテスト方針を定義する。

---

## 1. テスト対象

### 対象コンポーネント

- Go API
- PostgreSQL schema / migrations
- NATS / WebSocket 周辺
- MinIO storage API wrapper
- Next.js Admin UI
- backup/restore API

### 主要データ

- teams
- agents
- team_agents
- agent_relations
- threads
- tasks
- logs
- configs

---

## 2. 現行の自動テスト

### Go

実行コマンド:

```bash
go test ./...
```

一部テストは Testcontainers を使うため Docker が必要。

対象例:

- API handler
- credential generation
- team organization API
- config preset save/load/delete
- config restore replace
- thread snapshot export/import replace
- full snapshot export/import replace
- DB schema
- storage
- integration / e2e

### Admin UI

実行コマンド:

```bash
npm run lint
npm run build
```

Next build は Google Fonts 取得が必要になる場合がある。

---

## 3. API テスト項目

### 3.1 Health

| ID | 内容 | 期待結果 |
|---|---|---|
| API-HEALTH-001 | DB 接続が正常 | `200 {"status":"ok"}` |
| API-HEALTH-002 | DB 接続不可 | `503` |

### 3.2 Agents

| ID | 内容 | 期待結果 |
|---|---|---|
| API-AGENT-001 | agent 登録 | `201` |
| API-AGENT-002 | agent 登録時に team を要求しない | 無所属で作成 |
| API-AGENT-003 | id/name/role 欠落 | `400` |
| API-AGENT-004 | agent 更新 | `200` |
| API-AGENT-005 | agent 削除 | 関連 relation/thread/task/log は cascade |
| API-AGENT-006 | unknown agent credential 生成 | `404` |

### 3.3 Teams / Memberships

| ID | 内容 | 期待結果 |
|---|---|---|
| API-TEAM-001 | team 一覧取得 | `200 []` |
| API-TEAM-002 | team 作成 | `201` と team JSON |
| API-TEAM-003 | team 更新 | `200` |
| API-TEAM-004 | team 削除 | `204` |
| API-TEAM-005 | team 削除時の memberships/relations | cascade で削除 |
| API-TEAM-006 | team 削除時の threads | `team_id` が NULL |
| API-TEAM-007 | team に agent を追加 | `team_agents` に insert |
| API-TEAM-008 | 同じ agent を同じ team に再追加 | idempotent |
| API-TEAM-009 | team から agent を除外 | `team_agents` から delete |
| API-TEAM-010 | team から agent 除外時に同 team の relation も削除 | 整合性維持 |
| API-TEAM-011 | team 所属 agent 一覧 | `team_agents` ベースで返す |
| API-TEAM-012 | agent が複数 team に所属できる | 複数 `team_agents` が存在 |

### 3.4 Relations

| ID | 内容 | 期待結果 |
|---|---|---|
| API-REL-001 | 同一 team 所属 agent 間に boss relation 作成 | `201` |
| API-REL-002 | 同一 team 所属 agent 間に coworker relation 作成 | `201` |
| API-REL-003 | team 未所属 agent を含む relation 作成 | `400` |
| API-REL-004 | relation 削除 | `204` |
| API-REL-005 | team relation 一覧 | 指定 team の relation のみ返す |

### 3.5 Threads / Tasks

| ID | 内容 | 期待結果 |
|---|---|---|
| API-THREAD-001 | thread 作成 | `201` と thread id |
| API-THREAD-002 | team_id 指定で thread 作成 | `threads.team_id` に保存 |
| API-THREAD-003 | to/observers 指定 | `tasks.to_agents` / `tasks.observers` に保存 |
| API-THREAD-004 | thread 一覧 | `updated_at desc` |
| API-THREAD-005 | thread tasks 一覧 | created_at asc |
| API-THREAD-006 | thread 削除 | tasks/logs が cascade |
| API-TASK-001 | latest tasks 一覧 | 空の場合も `[]` |

---

## 4. Backup / Restore テスト項目

restore はすべて replace であり merge ではない。

### 4.1 Config snapshot

対象:

- teams
- agents
- team_agents
- agent_relations

| ID | 内容 | 期待結果 |
|---|---|---|
| SNAP-CONFIG-001 | current config を preset 保存 | configs に JSON 保存 |
| SNAP-CONFIG-002 | configs 一覧 | data を含まない metadata 一覧 |
| SNAP-CONFIG-003 | preset load | config が replace される |
| SNAP-CONFIG-004 | preset load 時に threads/tasks/logs が削除される | FK 整合性維持 |
| SNAP-CONFIG-005 | config export | teams/agents/team_agents/relations を含む |
| SNAP-CONFIG-006 | config import | transaction 内で replace |
| SNAP-CONFIG-007 | 旧 snapshot に team_agents が無い | agents.team_id から membership 補完 |

### 4.2 Thread snapshot

対象:

- threads
- tasks
- logs

| ID | 内容 | 期待結果 |
|---|---|---|
| SNAP-THREAD-001 | thread export | threads/tasks/logs を含む |
| SNAP-THREAD-002 | tasks の to_agents/observers を export | 欠落しない |
| SNAP-THREAD-003 | thread import | 既存 threads/tasks/logs を replace |
| SNAP-THREAD-004 | parent_thread_id あり | parent を復元 |
| SNAP-THREAD-005 | matching agent/team 不在 | FK エラーで rollback |
| SNAP-THREAD-006 | logs.id 復元後の sequence | 次回 insert が衝突しない |

### 4.3 Full snapshot

対象:

- configs
- teams
- agents
- team_agents
- agent_relations
- threads
- tasks
- logs

| ID | 内容 | 期待結果 |
|---|---|---|
| SNAP-FULL-001 | full export | 全対象テーブルを含む |
| SNAP-FULL-002 | full import | 全対象テーブルが replace |
| SNAP-FULL-003 | import 中に FK エラー | rollback |
| SNAP-FULL-004 | saved configs も復元 | configs が replace |
| SNAP-FULL-005 | export JSON で active state が configs より前に出る | 手編集時に `configs[].data` と誤認しにくい |
| SNAP-FULL-006 | top-level agents の mission/name/role/status を編集して import | active agents に反映 |

---

## 5. Admin UI テスト項目

### 5.1 Agents page

| ID | 内容 | 期待結果 |
|---|---|---|
| UI-AGENTS-001 | agent 一覧表示 | `/api/v1/agents` の内容を表示 |
| UI-AGENTS-002 | agent 登録フォーム | team 選択欄が無い |
| UI-AGENTS-003 | agent 詳細編集 | PATCH が成功 |
| UI-AGENTS-004 | agent 削除 | confirm 後 DELETE |

### 5.2 Org Tree

| ID | 内容 | 期待結果 |
|---|---|---|
| UI-ORG-001 | team 切り替え | graph が指定 team で更新 |
| UI-ORG-002 | Team プルダウンから新規作成 | `POST /teams` 後に作成 team を選択 |
| UI-ORG-003 | team mission 編集 | `PATCH /teams/:id` で保存 |
| UI-ORG-004 | team 削除 | confirm 後 `DELETE /teams/:id` |
| UI-ORG-005 | agent を team に追加 | membership API を呼ぶ |
| UI-ORG-006 | agent を team から除外 | confirm 後、relation も削除 |
| UI-ORG-007 | relation 作成 | 同一 team 所属 agent 間のみ成功 |
| UI-ORG-008 | backup panel が表示されない | graph 領域を圧迫しない |

### 5.3 Message Board

| ID | 内容 | 期待結果 |
|---|---|---|
| UI-BOARD-001 | team 切り替え | 指定 team の thread/message のみ表示 |
| UI-BOARD-002 | task post 時に team_id を送る | selected team が thread に紐付く |
| UI-BOARD-003 | thread 削除 | confirm 後 DELETE |
| UI-BOARD-004 | backup panel が表示されない | board 領域を圧迫しない |

### 5.4 Settings

| ID | 内容 | 期待結果 |
|---|---|---|
| UI-SETTINGS-001 | `/settings` が表示される | 404 にならない |
| UI-SETTINGS-002 | config backup/restore が表示される | Export/Restore 操作可能 |
| UI-SETTINGS-003 | thread backup/restore が表示される | Export/Restore 操作可能 |
| UI-SETTINGS-004 | full backup/restore が表示される | Export/Restore 操作可能 |
| UI-SETTINGS-005 | destructive restore | confirm が表示される |
| UI-SETTINGS-006 | saved presets | save/load/delete 操作可能 |

---

## 6. DB / Migration テスト項目

| ID | 内容 | 期待結果 |
|---|---|---|
| DB-SCHEMA-001 | schema.sql 初期化 | 全テーブル作成 |
| DB-SCHEMA-002 | team_agents PK | 同一 team/agent の重複不可 |
| DB-SCHEMA-003 | team delete | team_agents / relations cascade |
| DB-SCHEMA-004 | agent delete | team_agents / relations / threads / tasks / logs cascade |
| DB-SCHEMA-005 | thread delete | child threads / tasks / logs cascade |
| DB-MIG-001 | `20260607_add_team_agents.sql` 適用 | table/index 作成 |
| DB-MIG-002 | 既存 agents.team_id から team_agents へ同期 | membership 補完 |

---

## 7. 非機能テスト

| ID | 内容 | 合否基準 |
|---|---|---|
| NFT-BUILD-001 | Admin UI build | `npm run build` 成功 |
| NFT-LINT-001 | Admin UI lint | `npm run lint` 成功 |
| NFT-GO-001 | Go 全体テスト | `go test ./...` 成功 |
| NFT-RESTORE-001 | full restore 失敗時 | DB 変更が rollback |
| NFT-WS-001 | WebSocket 再接続 | UI 初期 fetch と WS update で状態復元 |

---

## 8. CI 注意点

- Go API tests は Testcontainers を使うため Docker が必要
- Next build は Google Fonts 取得で network が必要になる場合がある
- `npm run build` 成功時、`/settings` route が生成されることを確認する

---

## 9. 現行品質基準

- agent 登録と team 所属が分離されていること
- agent が複数 team に所属できること
- Org Tree が team 構成の唯一の UI であること
- Message Board が team を指定して thread 作成できること
- backup/restore は Settings に集約されていること
- config / threads / full restore が replace であること
- thread backup が messages/tasks/logs を欠落なく含むこと

---

## 11. 次期改善テスト項目（Step 0〜7 実装済み検証）

改善案（Step 0〜7）の実装に伴い、新しく自動テストおよび結合テスト（e2e/integration）に追加された検証ケースを定義する。

### 11.1 Step 4 & 5: ロール権限 ＆ サブスレッド起立

| ID | テスト内容 | 期待結果 |
|---|---|---|
| TEST-ROLE-001 | `TeamManager` がトップレベルスレッドを作成 | `201 Created`（作成可能） |
| TEST-ROLE-002 | `Chef` がトップレベルスレッドを作成 | `403 Forbidden`（作成不可、TeamManagerのみ） |
| TEST-ROLE-003 | `Worker` がトップレベル・サブスレッドを作成 | `403 Forbidden`（一律作成不可） |
| TEST-CHEF-001 | `Chef` が自身が所属するチームの親スレッド下にサブスレッドを作成 | `201 Created`（作成可能） |
| TEST-CHEF-002 | `Chef` が自身が所属していないチームの親スレッド下にサブスレッドを作成 | `403 Forbidden`（チームスコープ制限エラー） |
| TEST-INHERIT-001 | サブスレッド作成時にチームIDを省略 | 親スレッドの `team_id` を自動的に継承 |
| TEST-INHERIT-002 | トップレベルスレッド作成時にチームIDを省略 | 作成エージェントが所属するチームの `team_id` を自動補足・紐付け |

### 11.2 Step 3 & 5: サーバーサイド決定論的メンションアサイン

| ID | テスト内容 | 期待結果 |
|---|---|---|
| TEST-MENTION-001 | 投稿本文に `@agent-id` メンションを含める | サーバー側で自動抽出され、NATSメッセージの `to` 配列に設定 |
| TEST-MENTION-002 | 投稿本文にメンションを全く含めない | `400 Bad Request`（`NO_RECIPIENT` エラー）で投稿拒否 |
| TEST-MENTION-003 | 投稿本文に存在しないエージェントへのメンションを含める | `400 Bad Request`（`UNKNOWN_MENTION` エラー）で投稿拒否 |
| TEST-MENTION-004 | 投稿本文に `@team` メンションをチームコンテキスト付きで含める | チームメンバー全員に自動展開されてアサイン |

### 11.3 Step 6: 振り返り（Reflection）要求・登録

| ID | テスト内容 | 期待結果 |
|---|---|---|
| TEST-REFL-001 | スレッドに対する振り返りリクエストを発行 | 専用の振り返りサブスレッドが自動起立し、`thread_reflection_requests` レコード作成。関係者全員宛てのタスクメッセージが NATS にパブリッシュされる |
| TEST-REFL-002 | 振り返り評価の登録（正常系） | 同一チーム（上司・部下・同僚）のエージェント宛てにリフレクション評価を登録（`201 Created`） |
| TEST-REFL-003 | 振り返り評価の再投稿 | 同一リクエストID・評価者・被評価者・評価次元（`dimension`）で再投稿時に、最新の値で上書き更新（Upsert）される |
| TEST-REFL-004 | チーム外の無関係なエージェントへの評価登録 | `400 Bad Request`（`INVALID_TARGET_AGENT` エラー）で登録拒否 |
| TEST-REFL-005 | 締切期限切れ後の評価登録 | `400 Bad Request`（`REFLECTION_REQUEST_EXPIRED` エラー）で登録拒否 |

### 11.4 Step 7: 協働プロセス分析 ＆ KPI ダッシュボード

| ID | テスト内容 | 期待結果 |
|---|---|---|
| TEST-KPI-001 | スレッド別 KPI 取得（`GET /threads/:id/kpi`） | 指定親スレッドおよびそのサブスレッドすべてのメッセージ数、返答遅延秒数、未返答率、リフレクションスコアが正しく集計されて返却（`200 OK`） |
| TEST-KPI-002 | チーム別 KPI 取得（`GET /teams/:id/kpi`） | 該当チーム内のすべてのスレッド・サブスレッドを再帰集計した統計情報が正しく返却（`200 OK`） |
| TEST-KPI-003 | D3.js 用ネットワーク構造（`network_data`）の自動算出 | 送信メッセージ数を持つ `nodes` と、メンション回数を持つ `links`（`source`, `target`）が正しくシリアライズされて出力される |
| TEST-ANALYTICS-001 | KPI分析 Next.js プロダクションビルド | `npm run build` が一切のエラーなく成功し、`/analytics` ページが正常生成される |

