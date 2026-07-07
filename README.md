# discord-spam-ban-bot

短時間に同じ画像を複数チャンネルへ投稿するスパムを検出し、自動でBan(またはタイムアウト)するBot。

- 判定キーは「添付ファイル名 + サイズ」。ダウンロード不要で軽量
- 状態はTTL付きインメモリマップのみ。DB・Redis不要
- 常駐メモリ 〜20MB 程度。512MB VPSで十分動作

## 動作

デフォルトでは **60秒以内** に同一添付ファイルを **3つ以上の異なるチャンネル** に投稿したユーザーをBanし、直近1日分のメッセージを一括削除する。

除外対象(検出されない):

- Bot / Webhook
- サーバーオーナー、Administrator権限を持つメンバー
- `EXEMPT_ROLE_IDS` で指定したロールを持つメンバー

## セットアップ

### 1. Discord Developer Portal

1. [Developer Portal](https://discord.com/developers/applications) でアプリケーション作成 → Bot追加
2. **Bot → Privileged Gateway Intents → Message Content Intent を有効化**(添付ファイル情報の取得に必須)
3. OAuth2 URL Generator: scope `bot`、権限 `Ban Members` / `Moderate Members`(timeout利用時) / `Manage Messages` / `Send Messages` を選択して招待
4. サーバー設定で **Botのロールを対象ユーザーより上位** に配置する

### 2. ビルドと起動

```sh
go build -o spam-ban-bot .
DISCORD_TOKEN=xxxx ./spam-ban-bot
```

VPSへは `GOOS=linux GOARCH=amd64 go build -o spam-ban-bot .` でクロスビルドしてバイナリ1つ転送すればよい。

### 3. 環境変数

| 変数 | デフォルト | 説明 |
|---|---|---|
| `DISCORD_TOKEN` | (必須) | Botトークン |
| `SPAM_WINDOW_SECONDS` | `60` | 判定ウィンドウ(秒) |
| `SPAM_CHANNEL_THRESHOLD` | `3` | 発火する異なるチャンネル数 |
| `ACTION` | `ban` | `ban` または `timeout` |
| `TIMEOUT_MINUTES` | `1440` | timeout時の期間(分、最大28日) |
| `EXEMPT_ROLE_IDS` | (空) | 除外ロールID(カンマ区切り) |
| `LOG_CHANNEL_ID` | (空) | 処分ログを通知するチャンネルID |

導入直後は `ACTION=timeout` で数日様子を見て、誤検知がないことを確認してから `ban` に切り替えるのを推奨。

### 4. Docker

```sh
docker build -t spam-ban-bot .
docker run -d --name spam-ban-bot --restart always \
  -e DISCORD_TOKEN=xxxx \
  -e LOG_CHANNEL_ID=xxxx \
  --memory 128m \
  spam-ban-bot
```

distroless/staticベースでイメージは約8MB。シェルもパッケージマネージャも含まないので攻撃面も最小。

### 5. systemd(バイナリ直置きの場合)

`/etc/systemd/system/spam-ban-bot.service`:

```ini
[Unit]
Description=Discord spam ban bot
After=network-online.target
Wants=network-online.target

[Service]
ExecStart=/opt/spam-ban-bot/spam-ban-bot
Environment=DISCORD_TOKEN=xxxx
Environment=LOG_CHANNEL_ID=xxxx
Restart=always
RestartSec=5
DynamicUser=yes
MemoryMax=128M

[Install]
WantedBy=multi-user.target
```

```sh
systemctl enable --now spam-ban-bot
```

## 制限と拡張の余地

- 判定はファイル名+サイズの一致。ファイル名をランダム化してくるスパムがすり抜けた場合は、添付をダウンロードして SHA-256 で判定する実装に差し替える(`handleMessage` の `imageKey` 生成部分のみの変更で済む)
- 画像を微妙に加工してくるスパムには知覚ハッシュ(dHash等)が必要になるが、CPU/メモリコストが上がるため現状は非対応
- 埋め込みURL(リンクだけ貼るタイプ)のスパムは対象外
