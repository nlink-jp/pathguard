# pathguard

パスに触れてよいかの判定を 1 つに —— [nlink-jp](https://github.com/nlink-jp) のツール向け。

標準ライブラリだけで書いた Go のライブラリ。パスが、何も届いてはならない場所 —— システムのフォルダ、
資格情報の置き場所、エージェントの設定、サーバー自身のディレクトリ —— の中にあるかを判定する。判定は
**名前だけでなくファイルの実体でも**行うので、大文字小文字を区別しないディスクでの綴り違い、シンボリック
リンク、ファームリンクで通り抜けることはできない。

## パッケージ

| パッケージ | 中身 |
|---------|------------|
| `pathguard` | 汎用の判定。パスをリンク 1 段ずつ解決し、場所と実体・名前の両方で比べる。場所の一覧を 1 つ持ち、2 つのファイルの方針（Local と Outbound）を作る。MCP のことは知らない。 |
| `pathguard/workdir` | 組織 ADR-021（ファイルを受け渡す MCP サーバーの作業ディレクトリの契約）を `pathguard` の上に実装したもの。ツールの引数か `_meta` からの `work_dir`、閉じた検査の一覧、エラーコード。 |

## インストール

```bash
go get github.com/nlink-jp/pathguard
```

## 使い方

### サーバーが呼び出し側の作業ディレクトリを検査する

```go
import (
    "github.com/nlink-jp/pathguard"
    "github.com/nlink-jp/pathguard/workdir"
)

r := workdir.NewResolver(workdir.Options{
    Protected:    []pathguard.Place{pathguard.ServerDir(configDir, "")},
    RequiredHint: "Results come back as paths, and a path you cannot open is worth nothing.",
})

dir, err := r.Resolve(args.WorkDir, requestMeta) // 引数、無ければ _meta["jp.nlink/work_dir"]
var e *workdir.Error
if errors.As(err, &e) {
    // e.Code は work_dir_required / _invalid / _not_found / _not_writable / _denied
}
```

ゼロ値の `workdir.Resolver` はすべてを拒む —— 働くものを作るのは `NewResolver` だけ。ホームディレクトリが
分からなければ、すべての呼び出しを理由を添えて拒む。

### 呼び出しが名指すファイル

```go
// このマシンの上で読み書きする:
if reason, why := r.LocalPath(raw, resolved); why != "" { /* reason を添えて拒む */ }

// マシンの外へ送る（アップロード）:
if reason, why := r.OutboundPath(raw, resolved); why != "" { /* 拒む */ }
```

## 2 つのファイルの方針

| | Local（ここで読み書き） | Outbound（マシンの外へ送る） |
|---|---|---|
| **あなたの**ホームにある本物の資格情報・エージェント制御の場所（`~/.ssh`、`~/.aws`、`~/.kube`、`~/.config/gh`、`~/.netrc`、`~/.bash_history` など） | 拒む | 拒む |
| どこにあっても `.env` / `.env.*`（`.env.example`・`.sample`・`.template`・`.dist` を除く） | 拒む | 拒む |
| ほかの場所にある同じ名前のもの —— `evidence/home/bob/.bash_history`、プロジェクトの `.npmrc` | 通す | 拒む |
| どこにあっても秘密の名前 —— `id_rsa`、`credentials.json`、`*service-account*.json` | 通す | 拒む |
| サーバーが守るディレクトリ | 拒む | 拒む |

インシデント対応で集めた証拠の中の資格情報ファイルの写しは、分析する人が読む必要のあるものである。一方、
マシンの外へ出た鍵は取り返せない。システムのフォルダは作業ディレクトリを拒むもので、ファイルは拒まない。

## パスの比べ方

- **リンクの各段が「形」になる。** リンクは 1 段ずつ解決する。渡されたままのパス、途中のすべてのパス、
  最後のパスをすべて調べる。`~/.ssh/config` 自体が同期フォルダへのリンクのとき、`work/x → ~/.ssh/config`
  と仕込んだリンクは、途中の形が `~/.ssh` の中にあるので拒まれる。
- **実体と名前、常に両方。** 実体（形とその上にある存在するディレクトリに対する `os.SameFile`）は、存在する
  場所のあらゆる綴り —— 大文字小文字、リンク、ファームリンク、`~/.netrc` のようなファイルへのハードリンク ——
  を捕まえる。大文字小文字を同一視した名前の比較は、まだ存在しない場所と、実体の番号を信用できない
  ファイルシステムを覆う。
- **その場所そのものだけの項目は、そのものとだけ比べる。** `/`、`/private/var`、ホームは、それ*自体*である
  作業ディレクトリを拒み、その下すべては拒まない。

## 限界

これは床であって境界ではない。資格情報のディレクトリの中のファイルへの別名のハードリンクや、秘密の写しは、
Local の方針では検出しない。実体の番号が不安定なファイルシステムでは、実体の比較が衝突して正当なパスを
拒むことがある。

## 文書

- [RFP](docs/ja/pathguard-rfp.ja.md) —— 問題、判断、計画
- 組織 ADR-021・ADR-022（[nlink-jp/.github](https://github.com/nlink-jp/.github/tree/main/adr)）

## ライセンス

MIT
