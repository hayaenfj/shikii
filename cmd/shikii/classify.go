package main

// 版を上げる前の記事の埋め(0.9.0): 0.9.0 から記事には種類と場面が要る。前の版で作った記事には無いので、依頼の hook が見つけて
// classifyBatch 本ずつエージェントに付けさせ、終える時に埋まっていなければ作法の検査(wiki-kinds)が一度止める。付ける操作は
// shikii wiki classify(MCP なら shikii_wiki の classify)で、教訓の文は書き直さない。

import (
	"fmt"
	"strings"

	"github.com/hayaenfj/shikii/internal/notebook"
)

// classifyBatch は 1 回の依頼で頼む記事の数。
const classifyBatch = 5

// classifyContext は、種類か場面の無い記事があれば、その数と classifyBatch 本(id・題・教訓)と付け方を返す。無ければ空。
func classifyContext(agent, scope string) (string, []string, error) {
	if scope == "" {
		return "", nil, nil
	}
	book := notebook.Book{Root: agent, Scope: scope}
	un, err := book.Unclassified()
	if err != nil || len(un) == 0 {
		return "", nil, err
	}
	// wiki に読めない記事や手で直された記事があると保存は全部断られ、classify も必ず失敗する。頼まず(止めず)、lint を促す 1 行だけ(監査 0.9.0 の B2)
	// 止めるのは保存が断られる時(記事そのものの問題)だけ。別の記事の出典の問題では保存は通るので止めない(再監査 0.9.0 の新-2)
	if issues, err := book.PageIssues(); err == nil && len(issues) > 0 {
		return fmt.Sprintf("shikii: 種類と場面の無い記事が %d 本ありますが、wiki に読めない記事か手で直された記事があるので今は付けられません。%s wiki lint で問題を確かめて直すと、付けられるようになります。", len(un), hookExe()), nil, nil
	}
	live, err := book.Live()
	if err != nil {
		return "", nil, err
	}
	var scenes []string
	for _, sc := range liveScenes(live) {
		scenes = append(scenes, fmt.Sprintf("%s(%d)", sc.Scene, sc.Count))
		if len(scenes) == 12 {
			break
		}
	}
	batch := un
	if len(batch) > classifyBatch {
		batch = batch[:classifyBatch]
	}
	var b strings.Builder
	fmt.Fprintf(&b, "shikii: 種類と場面の無い記事が %d 本あります(版を上げる前に作った記事)。今の依頼の後に、次の %d 本の教訓を読んで種類と場面を付けます: %s wiki classify <記事ID> <種類> <場面>(MCP なら shikii_wiki の operation classify に id・kind・scene)。種類は rule=守る決まり / howto=やり方 / criteria=判断の基準 / fact=前提と事実 / history=経緯、場面はその仕事の言葉で「〜する時」(24 文字まで)。", len(un), len(batch), hookExe())
	if len(scenes) > 0 {
		b.WriteString("今ある場面: " + strings.Join(scenes, "、") + "。近い物があれば同じ言い方を使います。")
	}
	ids := make([]string, 0, len(batch))
	for _, p := range batch {
		fmt.Fprintf(&b, "\n- %s: %s — %s(当てはまる時: %s)", p.ID, p.Title, cut(p.Lesson, 160), cut(p.AppliesTo, 80))
		ids = append(ids, p.ID)
	}
	return b.String(), ids, nil
}

// classifyLeft は、頼んだ記事のうちまだ種類か場面の無い物の id。
func classifyLeft(root, scope string, asked []string) ([]string, error) {
	un, err := (notebook.Book{Root: root, Scope: scope}).Unclassified()
	if err != nil {
		return nil, err
	}
	want := map[string]bool{}
	for _, id := range asked {
		want[id] = true
	}
	var left []string
	for _, p := range un {
		if want[p.ID] {
			left = append(left, p.ID)
		}
	}
	return left, nil
}
