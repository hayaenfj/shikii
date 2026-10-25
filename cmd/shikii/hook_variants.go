package main

// 線が引けない判定の聞き方を、人に頼まずに試す。
// 正解が min_n に届いても線が引けなかった判定があれば、hook が依頼の直後にエージェントへ一度だけ頼む:
// 意味を変えない聞き方の候補を `shikii hook variants submit` へ渡す。shikii が候補を検査して spec の variants: に足し、
// 正解の貯金で採点する(tune、--write 無し)。今の質問を上回った候補があれば、Stop で人へ一度だけ伝える。
// 上回った候補の意味が同じかは、次の依頼の前にエージェントへ一度だけ頼み、人の設定の言語が英語以外なら訳を付けて `shikii hook variants check` で受け取る(0.9.2)。
// 同じ → その場で採用し(較正まで)、人へは(訳付きで)知らせて画面で元に戻せる。決めきれない → 和訳と違いを付けて人に聞く。違う → 採用しない。

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/hayaenfj/shikii/internal/spec"
	"github.com/hayaenfj/shikii/internal/store"
)

const (
	variantsMax        = 3    // 一度に受け取る候補の数
	variantQuestionMax = 2000 // 候補の質問の長さ(バイト)
)

var variantName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,39}$`)

type variantsRequest struct {
	Spec     string           `json:"spec"`
	Variants []variantRequest `json:"variants"`
}

type variantRequest struct {
	Name      string            `json:"name"`
	Question  string            `json:"question,omitempty"`
	Options   map[string]string `json:"options,omitempty"`
	Template  string            `json:"template,omitempty"`
	Rationale string            `json:"rationale,omitempty"` // 何を変えたか(記録しない。エージェントが考えを整理するため)
}

// stuckWording は、今の文言で正解が min_n に届き、自動較正を試したのに線が引けなかった判定(正解の数)。無ければ ok=false。
func stuckWording(agent, specID string) (n int, ok bool) {
	sp, err := spec.Load(hookSpecPath(agent, specID))
	if err != nil {
		return 0, false
	}
	guard := filepath.Join(agent, "calibrating-"+specID)
	info, err := os.Stat(guard)
	if err != nil || time.Since(info.ModTime()) < hookCalibrateGap {
		return 0, false // 試していないか、まだ走っているかもしれない
	}
	b, err := os.ReadFile(guard)
	if err != nil {
		return 0, false
	}
	var at, model, hash string
	if _, err := fmt.Sscanf(string(b), "%s %s %d %s", &at, &model, &n, &hash); err != nil || hash != sp.Hash() || n < sp.Eval.MinN {
		return 0, false
	}
	if c := sp.CalibrationFor(model); c != nil && c.SpecHash == sp.Hash() {
		return 0, false // 線が引けた
	}
	return n, true
}

// variantsContext は依頼の直後にエージェントへ渡す頼み(線が引けない判定ごとに、文言が変わるか正解が hookRecalibrateEvery 件増えるまで一度だけ)。
func variantsContext(agent string) string {
	var lines []string
	for _, id := range hookSpecs {
		n, ok := stuckWording(agent, id)
		if !ok {
			continue
		}
		sp, err := spec.Load(hookSpecPath(agent, id))
		if err != nil {
			continue
		}
		mark := filepath.Join(agent, "wording-asked-"+id)
		var hash string
		var last int
		if b, err := os.ReadFile(mark); err == nil {
			if _, err := fmt.Sscanf(string(b), "%s %d", &hash, &last); err == nil && hash == sp.Hash() && n < last+hookRecalibrateEvery {
				continue
			}
		}
		if err := os.WriteFile(mark, []byte(fmt.Sprintf("%s %d\n", sp.Hash(), n)), 0o600); err != nil {
			continue
		}
		lines = append(lines, variantsNotice(id, n, sp.Path()))
	}
	return strings.Join(lines, "\n")
}

func variantsNotice(specID string, n int, path string) string {
	command := "shikii"
	if exe, err := os.Executable(); err == nil {
		command = "'" + strings.ReplaceAll(exe, "'", "'\"'\"'") + "'"
	}
	example, _ := json.Marshal(variantsRequest{Spec: specID, Variants: []variantRequest{{Name: "clearer-yes", Question: "...", Rationale: "..."}}})
	return fmt.Sprintf("shikii: 判定「%s」は正解%d件でも線が引けません（今の質問とモデルでは答えを分けられていない）。人に頼まず、今の作業の区切りで、意味を変えない聞き方の候補を最大%d件作り、%s hook variants submit へJSONで渡してください。例: %s。questionのほかoptions（選択肢の説明の言い換え）とtemplateも使えます。今の質問は %s にあります。候補は正解の貯金で自動に測り、上回った候補は意味が同じかを後でもう一度確かめてもらいます。", specID, n, variantsMax, command, example, path)
}

func cmdHookVariants(args []string) error {
	if len(args) == 1 && args[0] == "check" {
		return cmdHookVariantsCheck()
	}
	if len(args) != 1 || args[0] != "submit" {
		return errors.New("使い方: shikii hook variants submit | check（JSONを標準入力）")
	}
	data, err := io.ReadAll(io.LimitReader(stdin, (16<<10)+1))
	if err != nil || len(data) > 16<<10 {
		return errors.New("候補は16KB以内のJSONにしてください")
	}
	var req variantsRequest
	if err := decodeStrict(data, &req); err != nil {
		return err
	}
	if !containsString(hookSpecs, req.Spec) {
		return fmt.Errorf("specは %s のどれかにしてください", strings.Join(hookSpecs, " / "))
	}
	if len(req.Variants) == 0 || len(req.Variants) > variantsMax {
		return fmt.Errorf("variantsは1〜%d件にしてください", variantsMax)
	}
	root, scope, err := currentProjectStore()
	if err != nil {
		return err
	}
	if active, err := projectActive(root, scope); err != nil {
		return err
	} else if !active {
		return errProjectInactive
	}
	path := hookSpecPath(root, req.Spec)
	sp, err := spec.Load(path)
	if err != nil {
		return err
	}
	var names []string
	for i, v := range req.Variants {
		if !variantName.MatchString(v.Name) {
			return fmt.Errorf("variants[%d].name は英小文字・数字・ハイフンの40文字以内にしてください", i)
		}
		if strings.TrimSpace(v.Question) == "" && len(v.Options) == 0 && strings.TrimSpace(v.Template) == "" {
			return fmt.Errorf("variants[%d] はquestion・options・templateのどれかを変えてください", i)
		}
		if len(v.Question) > variantQuestionMax {
			return fmt.Errorf("variants[%d].question は%dバイト以内にしてください", i, variantQuestionMax)
		}
		variant := spec.Variant{Name: v.Name, Question: strings.TrimSpace(v.Question), Options: v.Options, Template: v.Template}
		if err := sp.WithVariant(variant).Validate(); err != nil {
			return fmt.Errorf("variants[%d] は今の判定の形に合いません: %v", i, err)
		}
		sp.Variants = append(sp.Variants, variant)
		names = append(names, v.Name)
	}
	if err := sp.Validate(); err != nil {
		return fmt.Errorf("候補を足した判定を検査できません: %v", err)
	}
	if err := sp.Save(); err != nil {
		return err
	}
	startTune(root, sp)
	return json.NewEncoder(stdout).Encode(map[string]any{"saved": names, "tune": "started", "adoption": "上回った候補は、意味が同じかを次の依頼の前に確かめてもらってから"})
}

// decodeStrict は JSON を 1 つだけ、知らない鍵を拒んで読む。読めない時は、どの鍵がどの形であるべきかを返す。
func decodeStrict(data []byte, v any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(v); err != nil {
		return jsonShapeError(err)
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return errors.New("JSONは一つだけ渡してください")
	}
	return nil
}

// jsonShapeError は JSON の読み取り失敗を、直し方の分かる文にする。
func jsonShapeError(err error) error {
	var typeErr *json.UnmarshalTypeError
	var syntaxErr *json.SyntaxError
	switch {
	case errors.As(err, &typeErr):
		want := map[string]string{"struct": "オブジェクト {…}", "slice": "配列 […]", "string": "文字列", "bool": "true / false", "int": "整数", "map": "オブジェクト {…}"}[typeErr.Type.Kind().String()]
		if want == "" {
			want = typeErr.Type.String()
		}
		return fmt.Errorf("JSONの %s は%sで渡してください（%sが渡されました）", typeErr.Field, want, typeErr.Value)
	case errors.As(err, &syntaxErr):
		return fmt.Errorf("JSONの書き方が正しくありません（%dバイト目）", syntaxErr.Offset)
	case strings.HasPrefix(err.Error(), "json: unknown field "):
		return fmt.Errorf("JSONに知らない鍵があります: %s", strings.TrimPrefix(err.Error(), "json: unknown field "))
	}
	return fmt.Errorf("JSONを読み取れません: %v", err)
}

// startTune は聞き方の候補の採点を裏で起こし、自動 tune の控え(tuned-<spec>)を今の正解の数で書く(同じ数で重ねて回さない)。
func startTune(agent string, sp *spec.Spec) {
	st := store.Store{Dir: agent}
	n, err := humanTruthsFor(st, sp)
	if err != nil {
		hookLog("tune: " + err.Error())
		return
	}
	writeTuneGuard(agent, sp, n)
	hookTune(agent, sp.Path(), st.Dir)
}

func writeTuneGuard(agent string, sp *spec.Spec, n int) error {
	return os.WriteFile(filepath.Join(agent, "tuned-"+sp.ID), []byte(fmt.Sprintf("%d %s %s\n", n, sp.Hash(), time.Now().Format(time.RFC3339))), 0o600)
}

// wordingSummary は、聞き方の候補について人へ伝える行を Stop で一度だけ出す(run と状態ごとに 1 回。0.9.2)。
// エージェントが意味は同じと確かめて採用した時は(英語以外の設定なら訳付きで)知らせ、画面で元に戻せると書く。決めきれなかった時は訳と違いを付けて人に聞く。
// エージェントがまだ確かめていない時と、違うと言った時は人に何も言わない。
func wordingSummary(agent string) string {
	var lines []string
	for _, id := range hookSpecs {
		res, err := latestTuneResult(filepath.Join(agent, "wiki"), id)
		if err != nil {
			continue
		}
		check, ok := agentCheck(res)
		if !ok || personChecked(res) || res.Reverted {
			continue
		}
		var state, en, ja string
		url := projectPageURL(agent)
		switch {
		case res.AutoAdopted && res.Adopted:
			state = "adopted"
			en = fmt.Sprintf("shikii: the wording of %s was changed to %q: the agent checked it asks the same thing, and it matched more of your answers. If the meaning changed, undo it on the page: %s", id, res.Winner, url)
			ja = fmt.Sprintf("shikii: 判定「%s」の聞き方を「%s」に変えました(AI が意味は同じと確かめ、あなたの正解 %d 件で今より当たりました)。今まで: %s → これから: %s。意味が違っていたら画面で元に戻せます: %s", id, res.Winner, res.Truths, check.CurrentText, check.Candidate, pageRef(filepath.Dir(agent)))
		case check.Verdict == "unsure" && res.Adoptable && !res.Adopted:
			state = "unsure"
			en = fmt.Sprintf("shikii: a new wording %q for %s matched more of your answers, but the agent could not decide whether it asks the same thing. Please check it on the page: %s", res.Winner, id, url)
			ja = fmt.Sprintf("shikii: 判定「%s」の聞き方の候補「%s」は今より当たりましたが、意味が同じか AI が決めきれません(違いの見立て: %s)。今の聞き方: %s / 候補: %s。画面で意味が同じかを選んでください: %s", id, res.Winner, check.Difference, check.CurrentText, check.Candidate, pageRef(filepath.Dir(agent)))
		default:
			continue
		}
		if check.Lang != "" && check.Lang != "ja" && check.CurrentText != "" { // 日本語以外の設定では、英語の文に設定の言語の訳を添える
			en += fmt.Sprintf(" (%s — current: %s / new: %s)", langName(check.Lang), check.CurrentText, check.Candidate)
		}
		mark := filepath.Join(agent, "wording-shown-"+id)
		if b, err := os.ReadFile(mark); err == nil && strings.TrimSpace(string(b)) == res.Run+" "+state {
			continue
		}
		if err := os.WriteFile(mark, []byte(res.Run+" "+state+"\n"), 0o600); err != nil {
			continue
		}
		lines = append(lines, bi(en, ja, hookLoc{Lang: hookLang()}))
	}
	return strings.Join(lines, "\n")
}

// agentCheck はエージェントの意味の確かめ(最後の 1 件)。personChecked は人が画面か端末で確かめたか。
func agentCheck(res *tuneResult) (meaningCheck, bool) {
	for i := len(res.MeaningChecks) - 1; i >= 0; i-- {
		if res.MeaningChecks[i].By == "agent" {
			return res.MeaningChecks[i], true
		}
	}
	return meaningCheck{}, false
}

func personChecked(res *tuneResult) bool {
	for _, mc := range res.MeaningChecks {
		if mc.By != "agent" {
			return true
		}
	}
	return false
}

// wordingCheckContext は、今の質問を上回った候補がまだ誰にも確かめられていない時、意味が同じかと(英語以外の設定なら)訳をエージェントへ頼む文(run ごとに 1 回)。
func wordingCheckContext(agent string) string {
	var lines []string
	for _, id := range hookSpecs {
		sp, err := spec.Load(hookSpecPath(agent, id))
		if err != nil {
			continue
		}
		res, err := latestTuneResult(filepath.Join(agent, "wiki"), id)
		if err != nil || !res.Adoptable || res.Adopted || res.Reverted || len(res.MeaningChecks) > 0 || res.SpecHash != sp.Hash() || res.WinnerWording == nil || res.Current == nil {
			continue
		}
		mark := filepath.Join(agent, "wording-check-asked-"+id)
		if b, err := os.ReadFile(mark); err == nil && strings.TrimSpace(string(b)) == res.Run {
			continue
		}
		if err := os.WriteFile(mark, []byte(res.Run+"\n"), 0o600); err != nil {
			continue
		}
		lines = append(lines, wordingCheckNotice(res, hookLang()))
	}
	return strings.Join(lines, "\n")
}

func wordingCheckNotice(res *tuneResult, lang string) string {
	command := "shikii"
	if exe, err := os.Executable(); err == nil {
		command = "'" + strings.ReplaceAll(exe, "'", "'\"'\"'") + "'"
	}
	pair, _ := json.Marshal(map[string]any{"current": res.Current, "candidate": res.WinnerWording})
	req := wordingCheckRequest{Spec: res.SpecID, Run: res.Run, Verdict: "same | unsure | different", Difference: "意味の違い(無ければ空)"}
	translate, told := "", "人へ知らせます"
	if lang != "" { // 人の設定の言語が英語以外なら、両方をその言語に訳してもらう(英語なら訳は要らない)
		req.CurrentTranslation, req.CandidateTranslation = "今の質問の訳", "候補の訳"
		translate, told = "、両方を"+langName(lang)+"に訳して", "人へは訳付きで知らせます"
	}
	example, _ := json.Marshal(req)
	return fmt.Sprintf("shikii: 判定「%s」で聞き方の候補「%s」が今の質問より人の正解に合いました。今の作業の区切りで、2 つの聞き方が同じことを聞いているか(どんな場合でも同じ答えが正しいか)を確かめ%s、%s hook variants check へJSONで渡してください。例: %s。同じなら shikii がその場で採用し、%s(画面で元に戻せる)。少しでも意味が変わる、または決めきれない時は unsure にし、difference に違いを書きます(人に聞きます)。明らかに違えば different。聞き方: %s", res.SpecID, res.Winner, translate, command, example, told, pair)
}

type wordingCheckRequest struct {
	Spec                 string `json:"spec"`
	Run                  string `json:"run"`
	Verdict              string `json:"verdict"`
	CurrentTranslation   string `json:"current_translation,omitempty"`
	CandidateTranslation string `json:"candidate_translation,omitempty"`
	Difference           string `json:"difference,omitempty"`
}

// langNames は訳を頼む時に書く言語の名前(無ければ言語コードのまま)。
var langNames = map[string]string{"ja": "日本語", "zh": "简体中文", "zh-TW": "繁體中文", "ko": "한국어", "de": "Deutsch", "es": "español", "fr": "français", "it": "italiano", "pt-BR": "português", "ru": "русский", "uk": "українська", "pl": "polski", "tr": "türkçe", "vi": "tiếng việt", "id": "bahasa indonesia"}

func langName(code string) string {
	if n := langNames[code]; n != "" {
		return n
	}
	return code
}

// cmdHookVariantsCheck はエージェントの意味の確かめと訳(人の設定の言語が英語以外の時は必須、英語なら使わない)を受け取り、同じなら採用する(shikii hook variants check、JSON を標準入力)。
func cmdHookVariantsCheck() error {
	data, err := io.ReadAll(io.LimitReader(stdin, (16<<10)+1))
	if err != nil || len(data) > 16<<10 {
		return errors.New("確かめは16KB以内のJSONにしてください")
	}
	var req wordingCheckRequest
	if err := decodeStrict(data, &req); err != nil {
		return err
	}
	if !containsString(hookSpecs, req.Spec) {
		return fmt.Errorf("specは %s のどれかにしてください", strings.Join(hookSpecs, " / "))
	}
	if req.Verdict != "same" && req.Verdict != "unsure" && req.Verdict != "different" {
		return errors.New("verdict は same / unsure / different のどれかにしてください")
	}
	req.CurrentTranslation, req.CandidateTranslation, req.Difference = strings.TrimSpace(req.CurrentTranslation), strings.TrimSpace(req.CandidateTranslation), strings.TrimSpace(req.Difference)
	lang := hookLang()
	if lang != "" && (req.CurrentTranslation == "" || req.CandidateTranslation == "") {
		return fmt.Errorf("current_translation と candidate_translation(今の質問と候補の%sの訳)は両方要ります。人に知らせる時に使います", langName(lang))
	}
	if lang == "" { // 英語の設定では訳を使わない
		req.CurrentTranslation, req.CandidateTranslation = "", ""
	}
	if req.Verdict != "same" && req.Difference == "" {
		return errors.New("unsure と different の時は difference に意味の違いを書いてください")
	}
	for _, text := range []string{req.CurrentTranslation, req.CandidateTranslation, req.Difference} {
		if len(text) > variantQuestionMax {
			return fmt.Errorf("訳と違いは%dバイト以内にしてください", variantQuestionMax)
		}
	}
	root, scope, err := currentProjectStore()
	if err != nil {
		return err
	}
	if active, err := projectActive(root, scope); err != nil {
		return err
	} else if !active {
		return errProjectInactive
	}
	res, err := latestTuneResult(filepath.Join(root, "wiki"), req.Spec)
	if err != nil {
		return fmt.Errorf("判定「%s」の聞き方の候補を測った結果がありません", req.Spec)
	}
	if res.Run != req.Run {
		return fmt.Errorf("最新の結果は %s です(run が違います)", res.Run)
	}
	if !res.Adoptable || res.Adopted || res.Reverted || len(res.MeaningChecks) > 0 {
		return errors.New("この結果は、もう確かめる物がありません(採用済み・却下済み・元に戻した、または上回った候補が無い)")
	}
	recordCheck(res, meaningCheck{Same: req.Verdict == "same", By: "agent", Verdict: req.Verdict, Lang: lang, CurrentText: req.CurrentTranslation, Candidate: req.CandidateTranslation, Difference: req.Difference})
	if req.Verdict != "same" {
		next := "人に画面で聞きます"
		if req.Verdict == "different" {
			next = "採用しません"
		}
		return json.NewEncoder(stdout).Encode(map[string]any{"adopted": false, "verdict": req.Verdict, "next": next})
	}
	var buf bytes.Buffer
	path, err := wordingAdopt(res, endpointFlags{local: hookLocalURL, model: res.Model}, root, "", &buf)
	if err != nil {
		saveTuneResult(res)
		appendRunNote(res.RunPage, fmt.Sprintf("**Not adopted:** %s", strings.SplitN(err.Error(), "\n", 2)[0]))
		return json.NewEncoder(stdout).Encode(map[string]any{"adopted": false, "verdict": req.Verdict, "reason": err.Error()})
	}
	res.AutoAdopted = true
	saveTuneResult(res)
	appendRunNote(res.RunPage, fmt.Sprintf("**Adopted** as %s after the agent checked the meaning; the person is told with translations and can undo it on the page.", path))
	return json.NewEncoder(stdout).Encode(map[string]any{"adopted": true, "verdict": req.Verdict, "path": path, "next": "人へ知らせます(画面で元に戻せる)"})
}

// hookSpecPath は、プロジェクトの問い id の今の版のファイル(0.9.2)。聞き方を採用すると <id>-v<N>.yaml が増えるので、
// 同じ id で版の一番大きいファイルを使う(serve の loadSpecs と同じ選び方)。元に戻した版(.reverted)は .yaml で終わらないので見ない。
// 読めないファイルは飛ばし、どれも読めなければ元の <id>.yaml を返す(呼ぶ側の Load が誤りを出す)。
func hookSpecPath(agent, id string) string {
	base := filepath.Join(agent, "decisions", id+".yaml")
	best, version := base, -1
	if sp, err := spec.Load(base); err == nil {
		version = sp.Version
	}
	files, _ := filepath.Glob(filepath.Join(agent, "decisions", id+"-v*.yaml"))
	for _, f := range files {
		if sp, err := spec.Load(f); err == nil && sp.ID == id && sp.Version > version {
			best, version = f, sp.Version
		}
	}
	return best
}

var wordingAdopt = adoptWording
