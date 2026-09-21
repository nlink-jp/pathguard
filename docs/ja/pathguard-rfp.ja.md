# RFP: pathguard — パスに触れてよいかの判定を 1 つに

- Status: 承認済み（2026-09-22）— 独立の設計レビュー 2 回と、運用者の 2026-09-22 の判断（1 モジュールに 2 層・ランタイムは後で・手元と外送りで方針を分ける）を受けて改訂。同日に実装の独立レビューを受け、指摘を反映（「実装レビューで変わったこと」の節）
- Date: 2026-09-22
- Series: lib-series
- 改める対象（了承後）: 組織 ADR-021 の §4・§7・§10。新しい組織 ADR-022 によって

## 問題

パスに触れてよいかを判定するコードが 2 種類あり、どちらも名前で判定している。

- **MCP サーバー 9 本**は組織 ADR-021 を実装している —— 呼び出し側の `work_dir`、閉じた検査の一覧、
  ファイル引数が決して届いてはならない場所の床 —— が、それぞれ自前の `internal/workdir` で（§10:「これらの
  サーバー間で共有する Go モジュールは置かない」）。8 本は取り込むパッケージ名と ADR 番号しか違わないコピーで、
  slack-mcp-extender の 1 本はさらにずれている。`.env` の規則が無く、検査の順が違い、システムのフォルダを
  すべての綴りで比べ、`Resolver` 型を持たない（`Resolve(arg, meta, serverDirs)`）。
- **gem-agent と lagent** は、もっと長い資格情報の一覧を持っている（`internal/sandbox`）。ファイル操作
  ツールで使い、Seatbelt のプロファイルもそこから作る。

2 つの一覧は中身が違う。サーバーの床はホームの 9 か所と `.env`。ランタイムの一覧はそれに加えて `.kube`、
`.config/gh`、`.azure`、`.terraform.d`、`.gemini`、`.config/mcp-bridge`、ファイルの `.netrc`、`.npmrc`、`.pypirc`、
`.git-credentials`、`.vault-token`、`.docker/config.json`、`.claude.json`、`.bash_history`、`.zsh_history`、
そしてどこにあっても秘密である名前（`id_rsa` ほかの鍵の名前、`credentials.json`、
`application_default_credentials.json`、`*service-account*.json`）を持つ。

サーバーは、大文字小文字を区別しないディスク（APFS）の上で名前を比べている。2026-09-22 に chrome-pilot-mcp で
見つかり、レビューでも改めて確かめた: `.ENV` は `.env` の規則を、`~/.SSH` は資格情報の床を、`/USERS/<name>` は
ホームの検査を、`/USR/local` はシステムのフォルダを、サーバー自身のディレクトリの大文字小文字違いは
`work_dir_denied` を通る。端点どうしをどう比べても塞がらない穴もある。レビューしたマシンでは `~/.ssh` は
実在するディレクトリだが、`~/.ssh/config` は同期フォルダの中へのリンクなので、`work/x → ~/.ssh/config` は
`~/.ssh` でもその下でもない場所へ解決される。ホームディレクトリが分からないとき、今の検査はすべてを通す。

## これは何か

Go モジュール `github.com/nlink-jp/pathguard` を 1 つ。標準ライブラリだけで書き、`go 1.23`（最も古い
利用側の版）とする。2 層からなる:

- **`pathguard`** —— 汎用の判定。パスがある場所の中にあるか（リンクは 1 段ずつ解決し、実体と名前で比べる）、
  場所の一覧を 1 つ、そこから作る 2 つの方針。MCP も作業ディレクトリも知らない。
- **`pathguard/workdir`** —— その上に ADR-021: 引数か `_meta` からの `work_dir`、エラーコード、存在と
  書き込み可否、使うサーバーが守るディレクトリ。

各サーバーは同じ取り込みパスのローカル `internal/workdir` パッケージを残す。そのため、作業ディレクトリを
**解決する**コードやパスを検査するコードは変わらない。検査器を**作る**数行だけが、モジュールの作り方に
切り替わる（今は 8 本が `Resolver{Denied: …}` と書き、そのテストが `Resolver{}` と書いている）。

### 2 つの方針

運用者の判断（2026-09-22）: サーバーが**このマシンの上で読み書き**してよいものと、**マシンの外へ送って**
よいものは、分けて判定する。外へ出た鍵は取り返せない一方、インシデント対応で集めた証拠の中の鍵の写しは、
分析する人がまさに読む必要のあるものだからである。

- **Local（手元）** —— 読み書き用（data-toolbox、pcap-analyzer、文字起こし、image-forge の入力、すべての
  書き込み）。拒むもの: **あなたの**ホームにある本物の場所 —— 統合した一覧の資格情報のディレクトリと
  ファイル、エージェントの制御ディレクトリ（`.config/gem-agent`、`.config/lagent`）—— を実体と名前で比べて。
  そして、どこにあっても `.env` / `.env.*`（コミットされるひな形を除く）。ほかの場所にある同じ名前のもの ——
  `evidence/home/bob/.bash_history`、プロジェクト自身の `.npmrc`、書き出した `service-accounts.json` —— は読める。
- **Outbound（外送り）** —— マシンの外へ出るファイル用（slack-mcp-extender のアップロード、chrome-pilot-mcp の
  `upload_file`）。Local に加えて、ランタイムの規則: 資格情報のディレクトリ名やファイル名がパスのどこかの
  部分に現れたら拒む（`.claude`、`.gemini`、`.codex` はホームの下のときだけ。プロジェクトの `.claude/` には
  そのプロジェクトのスキルが入るため）。秘密の名前も、どこにあっても拒む。

システムのフォルダと、その場所そのものだけの項目（`/`、`/private/var`、ホームそのもの）は `work_dir` を拒む。
どちらのファイルの方針にも含めない —— `/etc/hosts` を読むのは漏洩ではなく、書き込みはそもそも `work_dir` の
中に限られている。

### API の素描

```go
package pathguard

type Kind int // System, Credential, AgentControl, Protected

// Place は、パスがその中にあってはならない場所。そのディレクトリとその下すべて、
// または —— Exact のとき —— そのパスそのものだけ。
type Place struct {
    Path   string
    Exact  bool
    Kind   Kind
    Reason string // details.reason（例: "sensitive_path"、"server_dir"）
    Why    string // 拒むときの文
}

// Forms は p の調べるべき綴りをすべて返す: 渡されたまま、途中のすべてのパス（リンクを 1 段ずつ
// 置き換えたもの）、最後のパス。
func Forms(p string) []string

// ServerDir はサーバー自身のディレクトリを表す Place（reason は "server_dir"）。
func ServerDir(path, note string) Place

// Check は、paths のどれかのどれかの形が入っている最初の場所を返す。
func Check(places []Place, paths ...string) (reason, why string)

// Floor はホームディレクトリに対する一覧を 1 つ作る。ホームが空か相対パスなら
// ErrNoHome で、呼び出し側は何も調べない代わりに拒む。
func Floor(home string) ([]Place, error)

// Policy はファイルのパスを判定する。Local と Outbound の 2 つだけがある。
type Policy struct{ /* 場所、名前の規則 */ }
func Local(home string, protected ...Place) (Policy, error)
func Outbound(home string, protected ...Place) (Policy, error)
func (p Policy) Check(paths ...string) (reason, why string)
```

```go
package workdir // github.com/nlink-jp/pathguard/workdir

const MetaKey = "jp.nlink/work_dir"
const (CodeRequired = "work_dir_required"; CodeInvalid = "work_dir_invalid";
       CodeNotFound = "work_dir_not_found"; CodeNotWritable = "work_dir_not_writable";
       CodeDenied = "work_dir_denied")

type Error struct{ Code, Message string; Details map[string]any }

type Options struct {
    Home         string           // "" なら os.UserHomeDir()。それも失敗したら、すべての呼び出しを拒む
    Protected    []pathguard.Place // このサーバー自身のディレクトリと、守っているもの
    RequiredHint string           // このサーバーでのディレクトリの用途。work_dir_required に添える
}

// NewResolver だけが働く Resolver を作る。ゼロ値の Resolver はすべてを拒む（自分の
// ディレクトリを渡さずに作った検査器が、それを 1 つも拒まずに出荷されたことがある）。
func NewResolver(o Options) Resolver
func (r Resolver) Resolve(arg string, meta map[string]json.RawMessage) (string, error)
func (r Resolver) Validate(dir string) (string, error)
func (r Resolver) LocalPath(raw, resolved string) (reason, why string)    // Local + Protected
func (r Resolver) OutboundPath(raw, resolved string) (reason, why string) // Outbound + Protected

// Resolver を持たない呼び出し箇所（今は voice-scribe の transcribe）向け。方針は
// このプロセスのホームから作り、ホームが分からなければ拒む。
func Sensitive(paths ...string) string
func SensitiveOutbound(paths ...string) string
```

`work_dir_denied` は `details` に `{work_dir, resolved, reason}` を持つ。

8 本のコピーの文言はそのまま残し、`RequiredHint` が各サーバーの 1 文を運ぶ。slack-mcp-extender は共通の順序と
文言に移る。

## 設計判断

1. **リンクの各段が「形」である。** リンクは 1 段ずつ解決する（`Lstat`、`Readlink`）。渡されたままのパス、
   途中のパス全体、最後のパスをすべて調べる。前置部分にすぎないリンクの場所だけは調べない（そうしないと、
   darwin のすべての一時ディレクトリで `/var` が「その場所そのもの」の `/private/var` に当たる）。`work/x → ~/.ssh/config → <同期フォルダ>/config` は、
   中間の形が `~/.ssh` の中にあるので拒まれる。
2. **比べ方は 2 つ、常に両方。** 実体（形とその上にある存在するディレクトリに対する `os.SameFile`）は、
   存在する場所のあらゆる綴り —— 大文字小文字、リンク、正規化、`~/.netrc` のようなファイルの項目への
   ハードリンク —— を捕まえる。大文字小文字を同一視した名前の比較は、まだ存在しない場所と、実体の番号を
   信用できないファイルシステム（`use_ino` の無い macFUSE、smbfs）を覆う。そこでは実体だけにすると今より
   弱くなる。名前は APFS と同じやり方で同一視する —— ASCII の小文字化ではなく、1 文字が複数文字に展開される
   ものも含めた Unicode のケースフォールディング。同一視は区別するディスクで拒みすぎるが、床としてはそれで
   よい。
3. **その場所そのものだけの項目は、形そのものとだけ比べ、上の階層とは比べない** —— そうしないと、すべての
   一時ディレクトリを拒む。
4. **ホームは呼び出し側から受け取り、実体で比べる。** ランタイムの `homePrefixRe`（名前で `/users/…`、
   `/home/…`）は `/root`、`/private/var/root`、Windows のホーム、移した `$HOME` を見落とし、ほかの利用者の
   ホームに当たる。`Floor(home)` と実体の比較がそれに代わる。方針が守るのは、サーバーを動かしている
   アカウントのホームで、ほかの利用者の `.claude` を名前で守ることはなくなる。
5. **ホームが分からなければ拒む。** 今の検査はホームが見つからないと "" を返す —— すべてを通す。
   `Floor("")` はエラーで、相対パスのホームもエラーにする（床が作業ディレクトリの下にできてしまう）。検査器は
   理由を添えてすべての呼び出しを拒む。
6. **費用は 1 回の呼び出しで有界、何もキャッシュしない。** 各場所を 1 回 `Stat` し、各形の上の階層を 1 回
   たどり、比較はメモリ上で行う。キャッシュは、作った後に現れた場所を見落とす。
7. **標準ライブラリだけで書き、テストで固定する。** 利用側の 2 本は第三者の依存を持たないと約束している
   （運用者の判断〔2026-09-22〕に従い、nlink-jp のモジュールは可と読める文言に改める）。モジュールは、
   自分の `go.mod` に `require` が無いことを確かめるテストでその約束を守る。

### 実装レビューで変わったこと

公開前の実装の独立レビューで、穴が 5 つとテストの不足が見つかった。どの修正にも、修正が無ければ落ちる
テストがある。

- **Unicode の同一視。** `strings.ToLower` は `id_rſa`（U+017F）を通していた。APFS ではこれで `id_rsa` が
  開く。ディスクで実測: `ſ` = `s`、ケルビン記号 = `k`、`ﬆ` と `ﬅ` = `st`、`ß` = `ss`。比較の鍵は、1 対多の
  フォールディング（`ß`、`ẞ`、ラテン文字の合字）を先に展開し、各文字を単純フォールディングの輪の最小の文字に
  置き換えたものになった。テストが、動いているディスクに対してすべての組を確かめる。
- **リンクの行き先。** 場所の中にあるリンク自身の位置は守られていたが、行き先は守られていなかった ——
  `~/.ssh/config` の同期フォルダ側の写しは、その名前で渡せば読めた。システム以外のディレクトリの場所の
  直下にあるリンクの行き先も、場所として加えるようにした —— 1 段だけで、これは明記した限界である。
- **存在しない部分の後の `..`**（`work/missing/../link`）は名前でつないでおり、存在する側にあるリンクを
  たどっていなかった。整えた残りをもう一度解決するようにした。
- **相対パスのホーム**は、作業ディレクトリの下に床を作っていた。`ErrNoHome` にした。
- **`Resolver` を持たない呼び出し箇所**（voice-scribe の transcribe は `workdir.Sensitive` を呼ぶ）には、
  移る先のモジュールの関数が無かった。`Sensitive` と `SensitiveOutbound` を加え、ホームが分からなければ
  拒むようにした。
- **Windows**（サーバーはそこでもビルドされる）: Windows は `..` を名前で処理し、ボリューム相対の形もある
  ので、相対パスは `filepath.Abs` で絶対パスにする。名前の比較では、Windows が無視する末尾の `.` と空白を
  落とす。

### サーバーで変わること（各サーバーの変更履歴に書く）

- **Local（読み書き）: 新たに拒む** —— 統合した一覧のうち、あなたのホームにある本物の場所（`~/.kube`、
  `~/.config/gh`、`~/.azure`、`~/.terraform.d`、`~/.gemini`、`~/.config/mcp-bridge`、`~/.netrc`、`~/.npmrc`、
  `~/.pypirc`、`~/.git-credentials`、`~/.vault-token`、`~/.docker/config.json`、`~/.claude.json`、
  `~/.bash_history`、`~/.zsh_history`）と、床のどの場所についても大文字小文字違いとリンク経由の道すべて。
  **新たに通す** —— `.env.example`、`.env.sample`、`.env.template`、`.env.dist`。
- **Outbound（アップロード）: さらに新たに拒む** —— 資格情報のディレクトリ名やファイル名がパスのどこかの
  部分に現れるもの、どこにあっても秘密の名前。slack-mcp-extender は `.env` の規則を得る（今は
  `allow_hidden=true` のとき `.env` をアップロードする）。その封じ込めのテスト 3 本は、`.env` について隠し
  ファイルではなく `sensitive_path` を返すようになる。
- **`work_dir`**: ホームディレクトリが分からないときと、Linux の `/etc` も拒む。

### 受け入れる限界（床は床のまま）

- 床の**ディレクトリ**の中のファイル（例: `~/.ssh/id_rsa`）への別名のハードリンクや、秘密の写しは、Local の
  方針では検出しない。
- 実体の番号が不安定なファイルシステムでは、実体の比較が衝突して正当なパスを拒むことがある。そこで守りに
  なるのは名前の比較である。
- Unicode の正規化は、存在する場所については（実体の比較で）扱い、非 ASCII のホームの下のまだ存在しない
  場所については扱わない。
- 行き先までたどるのは、場所の直下にあるリンクだけ。もっと深いところのリンクは、リンク自身の位置は守られる
  が、行き先は守られない。
- ほかの利用者の `.claude`・`.gemini`・`.codex` は守らない（判断 4）。写しのファイルは、これをランタイムとの
  意図した違いとして記録している。

### ランタイムの一覧とこの一覧をそろえておく

ランタイムがモジュールに載るまでは、ランタイムにとってはランタイムの一覧が正である。モジュールは両者の
`internal` パッケージを取り込めないので:

- `testdata/runtime-lists.json` にランタイムの 4 つの一覧（`credentialDirs`、`homeOnlyDirs`、`credentialFiles`、
  `credentialNames`）と、Outbound の方針が返すべき判定のパスの表（部分一致、ホームのみ、ひな形の各場合）を
  置く —— 判定で持つのは、正規表現は場所の一覧に「含む」ことができないためである。
- `check-org.sh` の検査で、両ランタイムの `lane.go` を `go/ast` で読み、どちらかが写しと違えば落とす ——
  3 つのリポジトリをすべて見られるのはそこだけである。

### 範囲外と、次

- サーバー自身の封じ込め（chrome-pilot-mcp の `confine.go`、slack-mcp-extender の `containment`）はサーバーに
  残し、`LocalPath` / `OutboundPath` を呼ぶ。chrome-pilot は守っているブラウザのプロファイルを守る場所として
  渡し、自前の 2 層目の実体の検査を捨てる。
- **gem-agent と lagent は、後の別の段階で `pathguard` に載る**（運用者、2026-09-22）。一覧から Seatbelt の
  プロファイルも作るため。Seatbelt の名前の規則が大文字小文字を同一視するかは未実測で、その段階で扱う。
- サンドボックスのプロキシ（ADR-021 §7 が先送りした作業）。

## 開発計画

1. **モジュール**を `_wip/pathguard` で作る: 2 つの層、両方の形のサーバーのコピーのテストを移植、新しい
   テスト —— 大文字小文字違い（区別するディスクでは理由を書いて飛ばす）、実在する床のディレクトリの中の
   リンクになったファイル、その場所だけの項目と下すべての項目、分からないホーム、ゼロ値の `Resolver`、
   ひな形、同じパスに対する Local と Outbound、ランタイムの写しの判定表、`require` 無しの固定。独立レビュー。
   v0.1.0 として `nlink-jp/pathguard` を公開し、lib-series にサブモジュールとして加える。
2. **サーバー**を 1 本ずつリリース: voice-scribe（旧移植元）から始め、gem-scribe、image-forge（`dist` を
   作り直してから、それを同梱する image-forge-gui）、voice-studio-mcp、video-studio-mcp、data-toolbox-mcp、
   pcap-analyzer-mcp、chrome-pilot-mcp、slack-mcp-extender。
3. **組織**: ADR-021 の §4（サーバーが実際に行っている順 —— 拒否の検査が書き込み可否より先）、§7（統合した
   一覧、ひな形、2 つの方針）、§10（判定は 1 つのモジュールにある。移植方式は廃止）を改める ADR-022。
   CONVENTIONS とナレッジを更新。`check-org.sh` の検査 —— 利用側サーバーのテストでない Go ファイルに床の
   文字列が無いこと、すべての利用側がモジュールの最新タグを使っていること、ランタイムの写しが両方の
   `lane.go` と一致すること。
4. **ランタイム**（別の段階、後で）: gem-agent と lagent は、一覧とパスの判定を `pathguard` から得て、
   Seatbelt のプロファイルもそこから作る。
