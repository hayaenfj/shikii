package main

// 線が引けない判定の聞き方を、人に頼まずに試す。
// 正解が min_n に届いても線が引けなかった判定があれば、hook が依頼の直後にエージェントへ一度だけ頼む:
// 意味を変えない聞き方の候補を `shikii hook variants submit` へ渡す。shikii が候補を検査して spec の variants: に足し、
// 正解の貯金で採点する(tune、--write 無し)。今の質問を上回った候補があれば、Stop で人へ一度だけ伝える。
// 採用は従来どおり人が意味を確かめてから(画面)。候補を出すのも測るのも機械で、人がするのはその確認 1 回。

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
	sp, err := spec.Load(filepath.Join(agent, "decisions", specID+".yaml"))
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
		sp, err := spec.Load(filepath.Join(agent, "decisions", id+".yaml"))
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
	return fmt.Sprintf("shikii: 判定「%s」は正解%d件でも線が引けません（今の質問とモデルでは答えを分けられていない）。人に頼まず、今の作業の区切りで、意味を変えない聞き方の候補を最大%d件作り、%s hook variants submit へJSONで渡してください。例: %s。questionのほかoptions（選択肢の説明の言い換え）とtemplateも使えます。今の質問は %s にあります。候補は正解の貯金で自動に測り、上回った候補の採用だけを人が意味を確かめて決めます。", specID, n, variantsMax, command, example, path)
}

func cmdHookVariants(args []string) error {
	if len(args) != 1 || args[0] != "submit" {
		return errors.New("使い方: shikii hook variants submit（JSONを標準入力）")
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
	path := filepath.Join(root, "decisions", req.Spec+".yaml")
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
	return json.NewEncoder(stdout).Encode(map[string]any{"saved": names, "tune": "started", "adoption": "人が画面で意味を確かめてから"})
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

// wordingSummary は、今の質問を上回り採用できる聞き方の候補を、Stop で人へ一度だけ伝える行(run ごとに 1 回)。
func wordingSummary(agent string) string {
	var lines []string
	for _, id := range hookSpecs {
		sp, err := spec.Load(filepath.Join(agent, "decisions", id+".yaml"))
		if err != nil {
			continue
		}
		res, err := latestTuneResult(filepath.Join(agent, "wiki"), id)
		if err != nil || !res.Adoptable || res.Adopted || len(res.MeaningChecks) > 0 || res.SpecHash != sp.Hash() {
			continue
		}
		mark := filepath.Join(agent, "wording-shown-"+id)
		if b, err := os.ReadFile(mark); err == nil && strings.TrimSpace(string(b)) == res.Run {
			continue
		}
		if err := os.WriteFile(mark, []byte(res.Run+"\n"), 0o600); err != nil {
			continue
		}
		lines = append(lines, bi(
			fmt.Sprintf("shikii: a candidate wording %q beat the current question of %s on the stored truths. Only check that it asks the same thing, then adopt or reject: %s", res.Winner, id, projectPageURL(agent)),
			fmt.Sprintf("shikii: 判定「%s」で聞き方の候補「%s」が今の質問を上回りました。意味が同じかだけ確かめて採用か却下を: %s", id, res.Winner, pageRef(filepath.Dir(agent))),
			hookLoc{Lang: hookLang()}))
	}
	return strings.Join(lines, "\n")
}
