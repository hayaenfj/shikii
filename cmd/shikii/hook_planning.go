package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/hayaenfj/shikii/internal/decide"
	"github.com/hayaenfj/shikii/internal/spec"
	"github.com/hayaenfj/shikii/internal/store"
)

// 文脈の整理は通常のエージェントが行う。hookは原文を控えるだけで、合意や未決事項を推測しない。
type planningFrame struct {
	TurnID  string            `json:"turn_id"`
	Sources map[string]string `json:"source_material"`
	At      time.Time         `json:"at"`
	Digest  string            `json:"digest,omitempty"`
	Result  *planningReceipt  `json:"result,omitempty"`
	Recent  []planningTurn    `json:"recent,omitempty"`
}

type planningQuote struct {
	Source string `json:"source"`
	Quote  string `json:"quote"`
}

type planningFact struct {
	Unknown  bool            `json:"unknown,omitempty"`
	Summary  string          `json:"summary,omitempty"`
	Evidence []planningQuote `json:"evidence,omitempty"`
}

type planningRequest struct {
	SessionID string        `json:"session_id"`
	Query     string        `json:"query,omitempty"`
	TurnID    string        `json:"turn_id"`
	Agreed    *planningFact `json:"agreed,omitempty"`
	Open      *planningFact `json:"open,omitempty"`
	Pages     []string      `json:"pages,omitempty"`
}

type planningReceipt struct {
	Recorded bool   `json:"recorded"`
	TraceID  string `json:"trace_id,omitempty"`
	Advice   string `json:"advice"`
	Shadow   bool   `json:"shadow"`
	Reason   string `json:"reason,omitempty"`
}

func needsPlanningContext(sp *spec.Spec) bool {
	have := map[string]bool{}
	for _, key := range sp.State.Required {
		have[key] = true
	}
	return sp.ID == "needs-plan" && have["request"] && have["agreed"] && have["open"]
}

func legacyPlanningRecord(sp *spec.Spec, rec store.Record) bool {
	return needsPlanningContext(sp) && (strings.TrimSpace(rec.Inputs["agreed"]) == "" || strings.TrimSpace(rec.Inputs["open"]) == "")
}

func newPlanningFrame(previous *hookSession, in *hookInput, scope, turnID string) *planningFrame {
	f := &planningFrame{TurnID: turnID, At: time.Now(), Sources: map[string]string{"current_user": cut(strings.TrimSpace(in.Prompt), 4000)}}
	if previous.Scope == scope {
		if previous.RawPrompt != "" {
			f.Sources["previous_user"] = cut(previous.RawPrompt, 4000)
		}
		if previous.Completed != nil && previous.Completed.TurnID == previous.TurnID {
			f.Sources["previous_assistant"] = cut(previous.Completed.Summary, 4000)
		}
	}
	addPlanningContext(scope, previous, f)
	return f
}

func planningNotice(sessionID, turnID string) string {
	ids, _ := json.Marshal(map[string]string{"session_id": sessionID, "turn_id": turnID})
	command := "shikii"
	if exe, err := os.Executable(); err == nil {
		command = "'" + strings.ReplaceAll(exe, "'", "'\"'\"'") + "'"
	}
	return fmt.Sprintf("shikii: 着手前の判断用に会話を整理します。人に入力を頼まず、%s hook plan queryへ次のJSONを渡して今回と直近の会話を読み、関係する決まりや既存の判断は、話題の語をqueryに付けて探し(依頼の文の写しにしない)、返った教訓を根拠に使ったらevidenceのsourceをlesson:<id>にします。同じID・検索語とagreed・openを%s hook plan submitへ渡してください。各項目はsummaryとevidence:[{source,quote}]（原文から引用）、不明ならunknown:true。合意の根拠には人の発言が必要です。整理は仮説で、実行許可や人の正解にはしません。新しく作った・変えた決まり・テスト・手順は、頼まれなくても終える前に wiki に書きます(近い記事に足すか直す)。詳細は作法のreferences/planning.md。\n", command, command) + string(ids)
}

func cmdHookPlan(args []string) error {
	if len(args) != 1 || (args[0] != "query" && args[0] != "submit") {
		return errors.New("使い方: shikii hook plan query | submit（JSONを標準入力）")
	}
	data, err := io.ReadAll(io.LimitReader(stdin, (16<<10)+1))
	if err != nil || len(data) > 16<<10 {
		return errors.New("着手前の文脈は16KB以内のJSONにしてください")
	}
	var req planningRequest
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		return fmt.Errorf("着手前の文脈を読み取れません: %v", jsonShapeError(err))
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF || req.SessionID == "" || len(req.SessionID) > 200 || len(req.TurnID) != 16 {
		return errors.New("現在のsession_idとturn_idを含むJSONを一つ渡してください")
	}
	if len(req.Query) > requestMax {
		return errors.New("wikiの検索語が長すぎます")
	}
	if _, err := hex.DecodeString(req.TurnID); err != nil {
		return errors.New("turn_idが不正です")
	}
	root, scope, err := currentProjectStore()
	if err != nil {
		return err
	}
	active, err := projectActive(root, scope)
	if err != nil {
		return err
	}
	if !active {
		return errProjectInactive
	}
	sessions, err := planningSessionRoot(root)
	if err != nil {
		return err
	}
	defer sessions.Close()
	name := filepath.Base(sessionPath(root, req.SessionID))
	s, err := readPlanningSession(sessions, name)
	if err != nil {
		return err
	}
	if err := currentPlanningFrame(s, req.TurnID, scope); err != nil {
		return err
	}
	if args[0] == "query" {
		notice, knowledge, err := preparePlanningKnowledge(root, scope, req, s.Planning.Sources)
		if err != nil {
			return err
		}
		if req.Agreed != nil || req.Open != nil {
			return errors.New("queryでは整理した内容を渡せません")
		}
		return json.NewEncoder(stdout).Encode(map[string]any{"session_id": req.SessionID, "turn_id": req.TurnID, "source_material": s.Planning.Sources, "context_notice": notice, "wiki_context": knowledge})
	}
	// 同時のsubmitは待たせず再試行にする。新しい人の依頼とStopは妨げない。
	busy, err := sessions.OpenFile(name+".planning.lock", os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0600)
	if err != nil {
		return err
	}
	defer busy.Close()
	if info, err := busy.Stat(); err != nil || !info.Mode().IsRegular() {
		return errors.New("文脈の錠を確認できません")
	}
	if err := syscall.Flock(int(busy.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return errors.New("同じ依頼の文脈を保存中です。再試行してください")
	}
	defer syscall.Flock(int(busy.Fd()), syscall.LOCK_UN)
	// 錠を取る前に別のsubmitや新しい依頼が終わった可能性を確認する。
	s, err = readPlanningSession(sessions, name)
	if err != nil {
		return err
	}
	if err := currentPlanningFrame(s, req.TurnID, scope); err != nil {
		return err
	}
	_, knowledge, err := preparePlanningKnowledge(root, scope, req, s.Planning.Sources)
	if err != nil {
		return err
	}
	agreed, err := renderPlanningFact(req.Agreed, s.Planning.Sources, true)
	if err != nil {
		return err
	}
	open, err := renderPlanningFact(req.Open, s.Planning.Sources, false)
	if err != nil {
		return err
	}
	planningCitations(req, knowledge)
	canonical, _ := json.Marshal(req)
	digest := sha256.Sum256(canonical)
	key := hex.EncodeToString(digest[:])
	if s.Planning.Result != nil && s.Planning.Digest == key {
		return json.NewEncoder(stdout).Encode(s.Planning.Result)
	}
	if s.Planning.Result != nil && s.Planning.Result.Recorded {
		return errors.New("この依頼の文脈は既に判定済みです")
	}
	result := &planningReceipt{Shadow: true}
	var d *decide.Decision
	sp, err := spec.Load(hookSpecPath(root, "needs-plan"))
	if err != nil {
		return err
	}
	if !needsPlanningContext(sp) {
		return errors.New("現在の質問は合意と未決事項の入力を使いません")
	}
	inputs := map[string]string{"request": cut(s.Planning.Sources["current_user"], requestMax), "agreed": agreed, "open": open}
	if req.Agreed.Unknown && req.Open.Unknown {
		result.Reason = "合意と未決事項が不明なため、判定を見送りました。人の答えには数えません。"
	} else {
		ctx, cancel := context.WithTimeout(context.Background(), hookDecideTimeout)
		defer cancel()
		d, err = decide.Decide(ctx, sp, inputs, decide.Session{}, hookEndpoints())
		if err != nil {
			// 判定モデルが混雑・停止していても整理は保存する(作法の検査に止められない)。判定は見送り、人の答えにも数えない(0.8.1)
			hookLog("plan submit: " + err.Error())
			d = nil
			result.Reason = "判定モデルに届かなかったため、整理は保存し、判定は見送りました。人の答えには数えません。"
		} else if d.Band == decide.Abstain {
			return errors.New("判定の入力が揃っていません")
		}
	}
	if d != nil {
		trace := sha256.Sum256([]byte("planning\x00" + scope + "\x00" + req.TurnID + "\x00" + sp.Hash() + "\x00" + key))
		d.TraceID = hex.EncodeToString(trace[:8])
		result.Recorded, result.TraceID, result.Shadow = true, d.TraceID, !d.IsCalibrated
		attachReceipt(sp, store.Store{Dir: root}, d)
		out := hookOutcome("needs-plan", d, localize(sp, d, hookLang()))
		result.Advice = joinLines(out.Context, out.System)
		result.Advice = strings.ReplaceAll(result.Advice, hookPageURL+"/", pageRef(filepath.Dir(root)))
	}
	// モデルを待つ間に依頼が進んだら、古い文脈を記録も助言もせず破棄する。
	err = savePlanningResult(sessions, name, root, scope, req, sp, d, inputs, result, key, knowledge)
	if err != nil {
		return err
	}
	return json.NewEncoder(stdout).Encode(result)
}

func renderPlanningFact(f *planningFact, sources map[string]string, agreement bool) (string, error) {
	if f == nil {
		return "", errors.New("agreedとopenを渡してください。不明ならunknown:trueを指定します")
	}
	if f.Unknown {
		if f.Summary != "" || len(f.Evidence) != 0 {
			return "", errors.New("不明な項目に合意や根拠を補わないでください")
		}
		return "不明（現在の会話から確かめられない）", nil
	}
	if strings.TrimSpace(f.Summary) == "" || len(f.Summary) > 600 || len(f.Evidence) == 0 || len(f.Evidence) > 3 {
		return "", errors.New("整理は600バイト以内、根拠の引用は1〜3件にしてください")
	}
	parts := []string{"AIによる整理（実行許可や人の正解ではない）: " + f.Summary}
	human := false
	for i, e := range f.Evidence {
		text, ok := sources[e.Source]
		switch {
		case !ok:
			keys := make([]string, 0, len(sources))
			for k := range sources {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			return "", fmt.Errorf("根拠evidence[%d]の出典 %q はこの依頼の原文にありません。使える出典: %s", i, e.Source, strings.Join(keys, ", "))
		case strings.TrimSpace(e.Quote) == "" || len(e.Quote) > 240:
			return "", fmt.Errorf("根拠evidence[%d]の引用は1〜240バイトにしてください（%dバイト）", i, len(e.Quote))
		case !strings.Contains(text, e.Quote):
			return "", fmt.Errorf("根拠evidence[%d]の引用が出典 %s の原文にそのまま見つかりません。書式や記号を含め、queryで返った原文から写してください", i, e.Source)
		}
		human = human || e.Source == "current_user" || e.Source == "previous_user"
		role := map[string]string{"current_user": "今回の人の発言", "previous_user": "前の人の発言", "previous_assistant": "直前のAIの返答"}[e.Source]
		if strings.HasPrefix(e.Source, "history:") {
			role = "同じセッションの過去の会話（参考・現在の合意ではない） [" + e.Source + "]"
		}
		if strings.HasPrefix(e.Source, "lesson:") || strings.HasPrefix(e.Source, "article:") {
			role = "wikiの教訓（出典付きの仮説・現在の合意ではない） [" + e.Source + "]"
		}
		parts = append(parts, role+": "+e.Quote)
	}
	if agreement && !human {
		return "", errors.New("AIの提案だけを合意と扱えません。人の発言の根拠が必要です")
	}
	return strings.Join(parts, "\n"), nil
}

func planningSessionRoot(root string) (*os.Root, error) {
	dir, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	info, err := dir.Lstat("sessions")
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("現在のセッションの保存先を確認できません")
	}
	return dir.OpenRoot("sessions")
}

func readPlanningSession(dir *os.Root, name string) (*hookSession, error) {
	f, err := dir.OpenFile(name, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, errors.New("そのセッションの記録がありません(session_id を確かめてください)")
		}
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > 1<<20 {
		return nil, errors.New("セッションの記録を確認できません")
	}
	var s hookSession
	decoder := json.NewDecoder(io.LimitReader(f, (1<<20)+1))
	if err := decoder.Decode(&s); err != nil {
		return nil, err
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return nil, errors.New("セッションは一つのJSONで保存してください")
	}
	return &s, nil
}

func currentPlanningFrame(s *hookSession, turnID, scope string) error {
	// Judged(ターンの終わりの判定が済んだ)後は受けないが、作法の検査が止めた直後(整理の submit がまだ無い)は受ける(0.8.0)
	if s.Scope != scope || s.TurnID != turnID || (s.Judged && s.Planning != nil && s.Planning.Result != nil) || s.Planning == nil || s.Planning.TurnID != turnID || time.Since(s.Planning.At) > hookSessionTTL {
		return errors.New("現在進行中の同じプロジェクト・同じ依頼の文脈だけを扱えます")
	}
	return nil
}

func savePlanningResult(dir *os.Root, name, root, scope string, req planningRequest, sp *spec.Spec, d *decide.Decision, inputs map[string]string, result *planningReceipt, digest string, knowledge *store.KnowledgeContext) error {
	lock, err := dir.OpenFile(name+".lock", os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if info, err := lock.Stat(); err != nil || !info.Mode().IsRegular() {
		return errors.New("セッションの錠を確認できません")
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return errors.New("依頼の記録を更新中です。再試行してください")
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	s, err := readPlanningSession(dir, name)
	if err != nil {
		return err
	}
	if err := currentPlanningFrame(s, req.TurnID, scope); err != nil {
		return err
	}
	fresh, err := spec.Load(sp.Path())
	if err != nil {
		return err
	}
	if fresh.Hash() != sp.Hash() {
		return errors.New("判定中に質問が変わりました。文脈を読み直してください")
	}
	if err := validatePlanningKnowledge(root, scope, knowledge); err != nil {
		return err
	}
	if d != nil {
		st := store.Store{Dir: root}
		records, err := st.Records("needs-plan", 0)
		if err != nil {
			return err
		}
		recorded := false
		for _, prior := range records {
			if prior.TraceID != d.TraceID {
				continue
			}
			if prior.SpecHash != d.SpecHash || prior.InputsHash != d.InputsHash {
				return errors.New("同じ依頼の記録を照合できません")
			}
			recorded = true
			// 記録の追記後に完了印の保存だけ失敗した再送。最初の予測を保ち、新しい助言は出さない。
			result.Advice, result.Shadow = "", true
			result.Reason = "保存済みの最初の判定を保持しました。"
			break
		}
		if !recorded {
			rec := recordOf(sp, d, inputs)
			rec.KnowledgeContext = knowledge
			rec.Project = filepath.Base(scope)
			l := localize(sp, d, hookLang())
			if l.Lang != "" {
				rec.Display = &store.RecordDisplay{Lang: l.Lang, Question: l.Question, Top: l.Top}
			}
			if err := recordDecisionRec(st, sp, d, rec); err != nil {
				return err
			}
		}
		if err := st.SaveRecordText(recordOf(sp, d, inputs), map[string]string{"request": s.Planning.Sources["current_user"]}); err != nil {
			return err
		}
		alreadyAsked := false
		for _, item := range s.Asked {
			alreadyAsked = alreadyAsked || item.Trace == d.TraceID
		}
		if !alreadyAsked && waitsForPersonFor("needs-plan", d) {
			s.Asked = append(s.Asked, askedItem{Spec: "needs-plan", Trace: d.TraceID})
		}
	}
	s.Planning.Digest, s.Planning.Result = digest, result
	s.At = time.Now()
	b, _ := json.MarshalIndent(s, "", "  ")
	temp := ".planning-" + store.NewTraceID() + ".tmp"
	f, err := dir.OpenFile(temp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer dir.Remove(temp)
	if _, err := f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := dir.Rename(temp, name); err != nil {
		return fmt.Errorf("文脈の保存: %w", err)
	}
	return nil
}
