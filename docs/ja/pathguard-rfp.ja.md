# RFP: pathguard — パスに触れてよいかの判定を 1 つに

- Status: 承認済み（2026-09-22）— 独立の設計レビュー 2 回と、運用者の 2026-09-22 の判断（1 モジュールに 2 層・ランタイムは後で・手元と外送りで方針を分ける）を受けて改訂。同日に実装の独立レビューを 3 回受け、指摘を反映（「実装レビューで変わったこと」の節）
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

// 絶対パスの無い場所は、すべての呼び出しを拒ませる（ErrBadPlace）。
var ErrBadPlace error

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
    Home         string           // "" なら $HOME と、それと違うときはアカウントのホーム（os/user）。どちらも分からなければ、すべての呼び出しを拒む
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
func (r Resolver) CheckBeneath(dir string) error                          // v0.2.0: <work_dir>/<workspace_id>

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
2. **比べ方は 2 つ、常に両方。** 実体の比較は「固定点」を使う。場所は、そのパスのうち存在するいちばん深い
   ところ（場所そのもの、または上位）と、その下に続く同一視した名前とで表す。形の側の存在する上位のどれかが
   同じファイル（`os.SameFile`）で、その下に続く名前が場所の名前で始まれば一致とする。これで、存在する部分の
   あらゆる綴り —— 大文字小文字、リンク、ファームリンク、`/.nofollow`、`/.vol`、正規化、`~/.netrc` のような
   ファイルの項目へのハードリンク —— を、場所が存在してもまだ存在しなくても捕まえる。同一視した名前の比較は、
   実体の番号を信用できないファイルシステム（`use_ino` の無い macFUSE、smbfs）を覆う。そこでは実体だけに
   すると今より弱くなる。名前は APFS と同じやり方で同一視する —— ASCII の小文字化ではなく、1 文字が複数文字に展開される
   ものも含めた Unicode のケースフォールディング。同一視は区別するディスクで拒みすぎるが、床としてはそれで
   よい。
3. **その場所そのものだけの項目は、形そのものとだけ比べ、上の階層とは比べない** —— そうしないと、すべての
   一時ディレクトリを拒む。
4. **ホームは呼び出し側から受け取り、実体で比べる。** ランタイムの `homePrefixRe`（名前で `/users/…`、
   `/home/…`）は `/root`、`/private/var/root`、Windows のホーム、移した `$HOME` を見落とし、ほかの利用者の
   ホームに当たる。`Floor(home)` と実体の比較がそれに代わる。方針が守るのは、サーバーを動かしている
   アカウントのホームで、ほかの利用者の `.claude` を名前で守ることはなくなる。ホームを渡されなければ、それは
   `$HOME` と、それと違うときはユーザーデータベースにあるアカウントのホームである。`HOME` をほかへ向けて
   起動したサーバーでも、本物のホームは守られる。
5. **ホームが分からなければ拒む。** 今の検査はホームが見つからないと "" を返す —— すべてを通す。
   `Floor("")` はエラーで、相対パスのホームもエラーにする（床が作業ディレクトリの下にできてしまう）。検査器は
   理由を添えてすべての呼び出しを拒む。
6. **費用は 1 回の呼び出しで有界、何もキャッシュしない。** 1 回の検査で、各場所自身の形とその上位を 1 回ずつ
   調べ、資格情報のディレクトリはリンクを探して 1 回ずつ一覧し、各形の上の階層を 1 回たどる。stat そのものを
   除けば、手間は形の長さに比例する。どのシステムも開けない長さ（4096 バイト、Windows では 32 KiB）の形は、
   渡されたものでもリンクをたどって生じたものでも、何かを調べる前にそのパスを拒ませる。Apple Silicon での実測:
   現実的なパスでも許される最長のパスでも 1 回の検査が約 2 ms（`BenchmarkLocalCheck`、
   `BenchmarkLocalCheckLongestPath`）。最悪の場合 —— 39 個のリンクを仕込み、すべての形を上限まで伸ばしたもの
   —— で約 0.33 秒（`BenchmarkLocalCheckLinkChainAtTheCap`）。比較はメモリ上で行う。キャッシュは、作った後に現れた場所を見落とす。
7. **標準ライブラリだけで書き、テストで固定する。** 利用側の 2 本は第三者の依存を持たないと約束している
   （運用者の判断〔2026-09-22〕に従い、nlink-jp のモジュールは可と読める文言に改める）。モジュールは、
   自分の `go.mod` に `require` が無いことを確かめるテストでその約束を守る。

### 実装レビューで変わったこと

公開前の実装の独立レビュー 3 回で、穴とテストの不足が見つかった。どの修正も、修正を外すとテストが落ちる
ことを変異で確かめてある。ただし Windows の `filepath.Abs` は、ここでは動かせないので除く。

**1 回目のレビュー** —— 穴が 5 つ:

- **Unicode の同一視。** `strings.ToLower` は `id_rſa`（U+017F）を通していた。APFS ではこれで `id_rsa` が
  開く。ディスクで実測: `ſ` = `s`、ケルビン記号 = `k`、`ﬆ` と `ﬅ` = `st`、`ß` = `ss`。比較の鍵は、1 対多の
  フォールディング（`ß`、`ẞ`、ラテン文字の合字）を先に展開し、各文字を単純フォールディングの輪の最小の文字に
  置き換えたものになった。1 本のテストが表のすべての項目を確かめ、もう 1 本が、動いているディスクで作れる組を
  ディスクに対して確かめる。
- **リンクの行き先。** 場所の中にあるリンク自身の位置は守られていたが、行き先は守られていなかった ——
  `~/.ssh/config` の同期フォルダ側の写しは、その名前で渡せば読めた。システム以外のディレクトリの場所の
  直下にあるリンクの行き先も、場所として加えるようにした —— 1 段だけで、これは明記した限界である。
- **存在しない部分の後の `..`**（`work/missing/../link`）は名前でつないでおり、存在する側にあるリンクを
  たどっていなかった。整えた残りをもう一度解決するようにした。
- **相対パスのホーム**は、作業ディレクトリの下に床を作っていた。`ErrNoHome` にした。
- **`Resolver` を持たない呼び出し箇所**（voice-scribe の transcribe は `workdir.Sensitive` を呼ぶ）には、
  移る先のモジュールの関数が無かった。`Sensitive` と `SensitiveOutbound` を加え、ホームが分からなければ
  拒むようにした。
- **Windows**（サーバーのうち 6 本は Windows 版を出している）: Windows は `..` を名前で処理し、ボリューム
  相対の形もあるので、相対パスは `filepath.Abs` で絶対パスにする。名前の比較では、Windows が無視する末尾の
  `.` と空白を落とす。

**2 回目のレビュー** —— 高が 1 つ、中が 3 つ。高と中の 1 つは原因が同じだったので、1 件ずつではなく原因で
直した:

- **まだ存在しない場所を、綴りだけで比べていた。** 存在する親の綴りは限りが無い。ファームリンク、
  `/.nofollow/…`、`/.vol/<dev>/<ino>/…`、リンクになった `~/.config`、正規化の違う非 ASCII のホームのどれでも、
  本物の `~/.aws/credentials` や `~/.config/gem-agent/config.toml` が作れた。原因は、実体を持たない場所が
  あったこと。直し方は固定点（判断 2）で、次の点もこれで覆う。
- **床のディレクトリの中の行き先の無いリンク**（`~/.ssh/config` → 同期フォルダの、まだ無いファイル）は、
  行き先が守られておらず、そこにファイルを仕込めた。行き先を普通の場所として扱い、ほかのまだ無い場所と
  同じく固定点で比べる。
- **`Why` の無い場所は何も守っていなかった。** 呼び出し側はどれも空の文を「許可」と読むため。空の文言は
  既定の文言で埋め、絶対パスの無い場所は `ErrBadPlace` にしてすべての呼び出しを拒む。
- **Windows の正規化**が相対パスにしか効いていなかった。すべてのパスを `filepath.Abs` に通す。名前の規則でも
  ストリームの接尾辞（`.env::$DATA`）と末尾の `.`・空白を落とす。ルートから始まるリンクの行き先
  （`\Users\u`）はリンクと同じドライブに置き、ジャンクションはリンクとしてたどる。
- **低の指摘のうち採ったもの:**
  - 床のディレクトリの中の `/` やホームを指すリンクが、すべてを守る木にしていた。そうした行き先は飛ばし、
    サーバー自身のディレクトリの中のリンクはたどらない。
  - 作業ディレクトリが読めないときは、相対パスを拒む。
  - `$HOME` がほかを指すときは、アカウント自身のホームも守る。

**3 回目のレビュー** —— それまでの指摘はすべて閉じていることを確認。中が 1 つ:

- **パスの引数 1 つで数秒かかりえた。** `look` は各上位の下に続く名前を先頭への追加で作っており、区切りの数の
  2 乗に比例していた。長さの上限も無かった。120 KB のパスで 1 回の検査に 14.5 秒かかった。名前は 1 回だけ
  同一視して共有し、どのシステムも開けない長さのパスは拒むようにした（判断 6）。
- **文書: 検査してから使うまでの競合が書かれていなかった。** 受け入れる限界に加えた。
- **採らなかったもの:** Windows のパスの分岐（`absolute`、`joinTarget`）へのテスト用の差し込み口。`filepath` の
  Windows の振る舞い（`VolumeName`、`Abs`）は Windows の上にしか無いので、darwin での差し込み口は模造品を
  テストすることになり、コードをテストしない。文書では、これらの分岐は推論であると書いてある。

**3 回目のレビューの修正の再確認** —— 上限は渡されたパスにしかかかっていなかった:

- **リンクをたどるたびに形がいくらでも伸びえた。** 97 バイトのパスが、行き先が 1 KB の相対パスであるリンクを
  1 つ通るだけで、40 回たどるうちに 40 KB の形を生み、21 秒かかった。`look` もまだ前置部分をすべて整え直して
  おり（`filepath.Dir`）、2 乗に比例していた。上限を `forms` のすべての形にかけ、解決しないパスは形を 1 つも
  調べる前に拒むようにした。`look` と `step` は部分文字列でたどる（`ancestors`、`parent`、`child`）。これらが
  `filepath` と同じ文字列を返すことはテストで固定してある。
- **検討して採らなかったもの:** `look` を、存在しない最初の上位で止めること。最悪の場合はさらに減るが、パスに
  沿った存在は単調ではない。`/.vol/<dev>` は stat できないのに `/.vol/<dev>/<ino>` はできる（実測）。止めると、
  2 回目のレビューの `/.vol` の穴を閉じている固定点が失われる。
- **採らなかったもの:** リンクをたどって生じた形の上限を、整える前ではなく整えた後の長さで判定すること。
  整える前の長さで判定すると、行き先が 4 KB を超えるが整えると短くなるリンクを通る正当なパスを拒むことが
  ある。起こりうるのは Linux だけで（darwin では、たどったパスもリンクの行き先もそれぞれ 1024 バイト以下）、
  作為的な行き先に限られ、しかも拒む側に倒れる。整える前の文字列に上限をかければ、`step` がたどる量にも上限が
  つく。


### サーバーで変わること（各サーバーの変更履歴に書く）

- **Local（読み書き）: 新たに拒む** —— 統合した一覧のうち、あなたのホームにある本物の場所（`~/.kube`、
  `~/.config/gh`、`~/.azure`、`~/.terraform.d`、`~/.gemini`、`~/.config/mcp-bridge`、`~/.netrc`、`~/.npmrc`、
  `~/.pypirc`、`~/.git-credentials`、`~/.vault-token`、`~/.docker/config.json`、`~/.claude.json`、
  `~/.bash_history`、`~/.zsh_history`）と、床のどの場所についても、存在してもまだ存在しなくても、あらゆる綴り。
  **新たに通す** —— `.env.example`、`.env.sample`、`.env.template`、`.env.dist`。
- **Outbound（アップロード）: さらに新たに拒む** —— 資格情報のディレクトリ名やファイル名がパスのどこかの
  部分に現れるもの、どこにあっても秘密の名前。slack-mcp-extender は `.env` の規則を得る（今は
  `allow_hidden=true` のとき `.env` をアップロードする）。その封じ込めのテスト 3 本は、`.env` について隠し
  ファイルではなく `sensitive_path` を返すようになる。
- **`work_dir`**: ホームディレクトリが分からないときと、Linux の `/etc` も拒む。

### 受け入れる限界（床は床のまま）

- 判定はその時点の写しである。検査してから開くまでの間に作られたリンクは見えない。呼び出し側は検査した解決済みの
  パスを開き、書き込みを閉じ込める（`os.Root`、`O_NOFOLLOW`）。この競合を閉じるのは、サーバー自身の封じ込めで
  ある。
- 床の**ディレクトリ**の中のファイル（例: `~/.ssh/id_rsa`）への別名のハードリンクや、秘密の写しは、Local の
  方針では検出しない。
- 実体の番号が不安定なファイルシステムでは、実体の比較が衝突して正当なパスを拒むことがある。そこで守りに
  なるのは名前の比較である。
- Unicode の正規化は実体の比較で扱う。正規化せずに比べるのは、まだ存在しない場所の、存在するいちばん深い
  ディレクトリより下の名前だけである。影響するのは、非 ASCII の名前を持ち、まだ作られていない守るディレクトリ
  だけで、床の名前はすべて ASCII である。
- 行き先までたどるのは、資格情報・エージェント制御のディレクトリの直下にあるリンクだけ。もっと深いところの
  リンクは、リンク自身の位置は守られるが、行き先は守られない。
- Windows はプラットフォームの文書どおりの動作から推論したもので、実測ではない（実測できる Windows の機械が
  無い）。8.3 形式の短い名前は、存在する場所には実体の比較で届くが、Outbound の方針の名前だけの規則は
  すり抜ける。
- ほかの利用者の `.claude`・`.gemini`・`.codex` は守らない（判断 4）。写しのファイルは、これをランタイムとの
  意図した違いとして記録している。
- 利用側の存在漏れの点検のあとに記録し（2026-09-22）、総合的なリスク評価のうえで受け入れたもの（どれも資格情報を
  読めず、直すと利用側すべての再リリースが要る）: 資格情報のディレクトリの項目を通って `..` で抜けるパスは行き着く
  場所で判定するので、その項目がリンクかどうかが答えに出うる。`work_dir` の検証は ADR-022 §4 の順序のままなので、
  資格情報のディレクトリを指す `work_dir` は、それが存在するかで答えが変わる。資格情報のディレクトリの中のファイルや
  `.env` へのハードリンクは拒まない。非 ASCII のリンク先の名前を別の正規化で綴ると、捕まるのは存在するあいだだけ。
  判定のたびに場所を用意し直す（約 2.5 ms）。このとき直したのは歩いた終点だけで、`Where` がそれを返す —— リンクの
  連鎖が前に出た綴りに戻ると、`Forms` の末尾は終点ではないため。

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
