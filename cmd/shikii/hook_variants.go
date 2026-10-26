package main

// Try other wordings for a decision whose line cannot be drawn, without asking the person.
// If a decision could not get a line even after its truths reached min_n, the hook asks the agent once, right after a request:
// pass wording variants that keep the meaning to `shikii hook variants submit`. shikii checks them, adds them to the spec's variants:,
// and scores them on the saved truths (tune, without --write). If a variant beats the current question, the person is told once at Stop.
// Whether a winning variant means the same is asked of the agent once before the next request; if the person's language setting is not English, with translations, received by `shikii hook variants check` (0.9.2).
// Same → adopted on the spot (up to calibration), and the person is told (with translations) and can undo it on the page. Undecided → ask the person, with a translation and the difference. Different → not adopted.

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
	variantsMax        = 3    // number of variants accepted at once
	variantQuestionMax = 2000 // length of a variant's question (bytes)
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
	Rationale string            `json:"rationale,omitempty"` // what was changed (not recorded; it helps the agent organize its thinking)
}

// stuckWording is a decision whose truths reached min_n on the current wording and that tried auto-calibration but got no line (the number of truths). ok=false if none.
func stuckWording(agent, specID string) (n int, ok bool) {
	sp, err := spec.Load(hookSpecPath(agent, specID))
	if err != nil {
		return 0, false
	}
	guard := filepath.Join(agent, "calibrating-"+specID)
	info, err := os.Stat(guard)
	if err != nil || time.Since(info.ModTime()) < hookCalibrateGap {
		return 0, false // not tried, or it may still be running
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
		return 0, false // a line was drawn
	}
	return n, true
}

// variantsContext is the request passed to the agent right after a request (per decision without a line, once until the wording changes or truths grow by hookRecalibrateEvery).
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
	return fmt.Sprintf("shikii: the decision %q has no line even with %d truths (the current question and model cannot tell the answers apart). Without asking the person, at a break in the current work, write up to %d wording variants that keep the meaning and pass them as JSON to %s hook variants submit. Example: %s. Besides question you can also use options (rewordings of the choice descriptions) and template. The current question is in %s. Variants are measured automatically on the saved truths, and a winning variant will later be checked again for the same meaning.", specID, n, variantsMax, command, example, path)
}

func cmdHookVariants(args []string) error {
	if len(args) == 1 && args[0] == "check" {
		return cmdHookVariantsCheck()
	}
	if len(args) != 1 || args[0] != "submit" {
		return errors.New("usage: shikii hook variants submit | check (JSON on standard input)")
	}
	data, err := io.ReadAll(io.LimitReader(stdin, (16<<10)+1))
	if err != nil || len(data) > 16<<10 {
		return errors.New("variants must be JSON of 16KB or less")
	}
	var req variantsRequest
	if err := decodeStrict(data, &req); err != nil {
		return err
	}
	if !containsString(hookSpecs, req.Spec) {
		return fmt.Errorf("spec must be one of %s", strings.Join(hookSpecs, " / "))
	}
	if len(req.Variants) == 0 || len(req.Variants) > variantsMax {
		return fmt.Errorf("variants must have 1 to %d items", variantsMax)
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
			return fmt.Errorf("variants[%d].name must be at most 40 lowercase letters, digits and hyphens", i)
		}
		if strings.TrimSpace(v.Question) == "" && len(v.Options) == 0 && strings.TrimSpace(v.Template) == "" {
			return fmt.Errorf("variants[%d] must change one of question, options or template", i)
		}
		if len(v.Question) > variantQuestionMax {
			return fmt.Errorf("variants[%d].question must be %d bytes or less", i, variantQuestionMax)
		}
		variant := spec.Variant{Name: v.Name, Question: strings.TrimSpace(v.Question), Options: v.Options, Template: v.Template}
		if err := sp.WithVariant(variant).Validate(); err != nil {
			return fmt.Errorf("variants[%d] does not fit the current decision's shape: %v", i, err)
		}
		sp.Variants = append(sp.Variants, variant)
		names = append(names, v.Name)
	}
	if err := sp.Validate(); err != nil {
		return fmt.Errorf("cannot check the decision with the variants added: %v", err)
	}
	if err := sp.Save(); err != nil {
		return err
	}
	startTune(root, sp)
	return json.NewEncoder(stdout).Encode(map[string]any{"saved": names, "tune": "started", "adoption": "a winning variant is adopted after its meaning is checked before the next request"})
}

// decodeStrict reads exactly one JSON value and refuses unknown keys. When it cannot read it, it says which key should have which shape.
func decodeStrict(data []byte, v any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(v); err != nil {
		return jsonShapeError(err)
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return errors.New("pass only one JSON value")
	}
	return nil
}

// jsonShapeError turns a JSON read failure into text that says how to fix it.
func jsonShapeError(err error) error {
	var typeErr *json.UnmarshalTypeError
	var syntaxErr *json.SyntaxError
	switch {
	case errors.As(err, &typeErr):
		want := map[string]string{"struct": "an object {…}", "slice": "an array […]", "string": "a string", "bool": "true / false", "int": "an integer", "map": "an object {…}"}[typeErr.Type.Kind().String()]
		if want == "" {
			want = typeErr.Type.String()
		}
		return fmt.Errorf("pass the JSON %s as %s (got %s)", typeErr.Field, want, typeErr.Value)
	case errors.As(err, &syntaxErr):
		return fmt.Errorf("the JSON is malformed (at byte %d)", syntaxErr.Offset)
	case strings.HasPrefix(err.Error(), "json: unknown field "):
		return fmt.Errorf("the JSON has an unknown key: %s", strings.TrimPrefix(err.Error(), "json: unknown field "))
	}
	return fmt.Errorf("cannot read the JSON: %v", err)
}

// startTune starts scoring the wording variants in the background and writes the auto-tune record (tuned-<spec>) with the current number of truths (so it is not run again on the same number).
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

// wordingSummary outputs, once at Stop, the line telling the person about wording variants (once per run and state. 0.9.2).
// When the agent checked that the meaning is the same and adopted it, it says so (with translations for a non-English setting) and that it can be undone on the page. When undecided, it asks the person with the translation and the difference.
// It says nothing to the person when the agent has not checked yet or said it is different.
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
		title, enTitle := jaQuestionTitle(agent, id), enQuestionTitle(agent, id)
		// what changed, so the person can decide without opening the page (audit 0.9.2, low 4). The candidate is shown by its question, not its internal name (0.9.3)
		var both, onlyNew string
		if res.WinnerWording != nil {
			onlyNew = fmt.Sprintf(" (new: %s)", trim(res.WinnerWording.Question, wordingNoticeRunes))
			if res.Current != nil {
				both = fmt.Sprintf(" (current: %s / new: %s)", trim(res.Current.Question, wordingNoticeRunes), trim(res.WinnerWording.Question, wordingNoticeRunes))
			}
		}
		switch {
		case res.AutoAdopted && res.Adopted:
			state = "adopted"
			en = fmt.Sprintf("shikii: the wording of %q was changed: the agent checked it asks the same thing, and it matched more of the truths so far. If the meaning changed, undo it on the page: %s", enTitle, url) + both
			ja = fmt.Sprintf("shikii: 問い「%s」の聞き方を変えました(AI が意味は同じと確かめ、正解 %d 件で今より当たりました)。今まで: %s → これから: %s。意味が違っていたら画面で元に戻せます: %s", title, res.Truths, check.CurrentText, check.Candidate, pageRef(filepath.Dir(agent)))
		case check.Verdict == "unsure" && res.Adoptable && !res.Adopted:
			state = "unsure"
			en = fmt.Sprintf("shikii: a new wording for %q matched more of the truths so far, but the agent could not decide whether it asks the same thing (its view of the difference: %s). Please check it on the page: %s", enTitle, check.Difference, url) + both
			ja = fmt.Sprintf("shikii: 問い「%s」の聞き方の候補は今より当たりましたが、意味が同じか AI が決めきれません(違いの見立て: %s)。今の聞き方: %s / 候補: %s。画面で意味が同じかを選んでください: %s", title, check.Difference, check.CurrentText, check.Candidate, pageRef(filepath.Dir(agent)))
		case check.Verdict == "same" && res.Adoptable && !res.Adopted && res.AdoptError != "":
			state = "failed"
			en = fmt.Sprintf("shikii: the agent found that a new wording for %q asks the same thing, but it could not be adopted (%s). The current wording stays; it is tried again as truths grow", enTitle, res.AdoptError) + onlyNew
			ja = fmt.Sprintf("shikii: 問い「%s」の聞き方の候補は AI が意味は同じと確かめましたが、採用できませんでした(%s)。今の聞き方のままで、正解が増えた時にもう一度試します", title, res.AdoptError)
		default:
			continue
		}
		if check.Lang != "" && check.Lang != "ja" && check.CurrentText != "" { // for a non-Japanese setting, add the translation in the configured language to the English text
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

// jaQuestionTitle / enQuestionTitle are how the notices to the person name a question: the page's title (display.<lang>.title), else the page's question,
// never the spec id (audit 0.9.2 ui; English since 0.9.3).
func jaQuestionTitle(agent, id string) string { return questionTitle(loadOrNil(agent, id), "ja") }
func enQuestionTitle(agent, id string) string { return questionTitle(loadOrNil(agent, id), "en") }

func loadOrNil(agent, id string) *spec.Spec {
	sp, err := spec.Load(hookSpecPath(agent, id))
	if err != nil {
		return nil
	}
	return sp
}

func questionTitle(sp *spec.Spec, lang string) string {
	if sp != nil {
		if d, ok := sp.Display[lang]; ok {
			if t := strings.TrimSpace(d.Title); t != "" {
				return t
			}
			if q := strings.TrimSpace(d.Question); q != "" {
				return q
			}
		}
	}
	if lang == "ja" {
		return "この問い"
	}
	return "this question"
}

// wordingNoticeRunes is how much of each question the English notice shows (the whole question is on the page).
const wordingNoticeRunes = 120

// agentCheck is the agent's meaning check (the last one). personChecked reports whether the person checked on the page or in the terminal.
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

// wordingCheckContext is the request to the agent, when a variant that beat the current question has not been checked by anyone yet, to check whether it means the same and (for a non-English setting) to translate it (once per run).
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
		// Asked at most once a day per run: if the agent's call failed (e.g. it ran the command in another folder), it is asked again on a later day
		mark := filepath.Join(agent, "wording-check-asked-"+id)
		if b, err := os.ReadFile(mark); err == nil && strings.TrimSpace(string(b)) == res.Run {
			if info, err := os.Stat(mark); err == nil && time.Since(info.ModTime()) < 24*time.Hour {
				continue
			}
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
	req := wordingCheckRequest{Spec: res.SpecID, Run: res.Run, Verdict: "same | unsure | different", Difference: "the difference in meaning (empty if none)"}
	translate, told := "", "the person is told"
	if lang != "" { // if the person's language setting is not English, have both translated into that language (no translation needed for English)
		req.CurrentTranslation, req.CandidateTranslation = "translation of the current question", "translation of the variant"
		translate, told = ", translate both into "+langName(lang)+",", "the person is told with the translations"
	}
	example, _ := json.Marshal(req)
	return fmt.Sprintf("shikii: for the decision %q, the wording variant %q matched the person's truths better than the current question. At a break in the current work, check whether the two wordings ask the same thing (whether the same answer is right in every case)%s and pass it as JSON to %s hook variants check. Example: %s. If they are the same, shikii adopts it on the spot and %s (it can be undone on the page). If the meaning changes even slightly, or you cannot decide, use unsure and write the difference in difference (the person is asked). If clearly different, use different. Wordings: %s", res.SpecID, res.Winner, translate, command, example, told, pair)
}

type wordingCheckRequest struct {
	Spec                 string `json:"spec"`
	Run                  string `json:"run"`
	Verdict              string `json:"verdict"`
	CurrentTranslation   string `json:"current_translation,omitempty"`
	CandidateTranslation string `json:"candidate_translation,omitempty"`
	Difference           string `json:"difference,omitempty"`
}

// langNames is the language names written when asking for a translation (the language code if none).
var langNames = map[string]string{"ja": "日本語", "zh": "简体中文", "zh-TW": "繁體中文", "ko": "한국어", "de": "Deutsch", "es": "español", "fr": "français", "it": "italiano", "pt-BR": "português", "ru": "русский", "uk": "українська", "pl": "polski", "tr": "türkçe", "vi": "tiếng việt", "id": "bahasa indonesia"}

func langName(code string) string {
	if n := langNames[code]; n != "" {
		return n
	}
	return code
}

// cmdHookVariantsCheck receives the agent's meaning check and translations (required when the person's language setting is not English, unused for English) and adopts the variant if it is the same (shikii hook variants check, JSON on standard input).
func cmdHookVariantsCheck() error {
	data, err := io.ReadAll(io.LimitReader(stdin, (16<<10)+1))
	if err != nil || len(data) > 16<<10 {
		return errors.New("the check must be JSON of 16KB or less")
	}
	var req wordingCheckRequest
	if err := decodeStrict(data, &req); err != nil {
		return err
	}
	if !containsString(hookSpecs, req.Spec) {
		return fmt.Errorf("spec must be one of %s", strings.Join(hookSpecs, " / "))
	}
	if req.Verdict != "same" && req.Verdict != "unsure" && req.Verdict != "different" {
		return errors.New("verdict must be one of same / unsure / different")
	}
	req.CurrentTranslation, req.CandidateTranslation, req.Difference = strings.TrimSpace(req.CurrentTranslation), strings.TrimSpace(req.CandidateTranslation), strings.TrimSpace(req.Difference)
	lang := hookLang()
	if lang != "" && (req.CurrentTranslation == "" || req.CandidateTranslation == "") {
		return fmt.Errorf("both current_translation and candidate_translation (the %s translations of the current question and the variant) are needed. They are used when telling the person", langName(lang))
	}
	if lang == "" { // an English setting does not use translations
		req.CurrentTranslation, req.CandidateTranslation = "", ""
	}
	if req.Verdict != "same" && req.Difference == "" {
		return errors.New("for unsure and different, write the difference in meaning in difference")
	}
	for _, text := range []string{req.CurrentTranslation, req.CandidateTranslation, req.Difference} {
		if len(text) > variantQuestionMax {
			return fmt.Errorf("translations and the difference must be %d bytes or less", variantQuestionMax)
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
		return fmt.Errorf("there is no result of measuring wording variants for the decision %q", req.Spec)
	}
	if res.Run != req.Run {
		return fmt.Errorf("the latest result is %s (the run differs)", res.Run)
	}
	// the page (serve) may be adopting or undoing the same run: hold the run's lock while checking and adopting, and read the result again under it
	unlock, err := lockTuneRun(filepath.Join(root, "wiki"), res.Run)
	if err != nil {
		return err
	}
	defer unlock()
	if res, err = loadTuneResult(filepath.Join(root, "wiki"), req.Run); err != nil {
		return err
	}
	if !res.Adoptable || res.Adopted || res.Reverted || len(res.MeaningChecks) > 0 {
		return errors.New("this result has nothing left to check (adopted, rejected, undone, or no winning variant)")
	}
	recordCheck(res, meaningCheck{Same: req.Verdict == "same", By: "agent", Verdict: req.Verdict, Lang: lang, CurrentText: req.CurrentTranslation, Candidate: req.CandidateTranslation, Difference: req.Difference})
	if req.Verdict != "same" {
		next := "the person is asked on the page"
		if req.Verdict == "different" {
			next = "not adopted"
		}
		return json.NewEncoder(stdout).Encode(map[string]any{"adopted": false, "verdict": req.Verdict, "next": next})
	}
	var buf bytes.Buffer
	path, err := wordingAdopt(res, endpointFlags{local: hookLocalURL, model: res.Model}, root, "", &buf)
	if err != nil {
		res.AdoptError = strings.SplitN(err.Error(), "\n", 2)[0] // the person is told once at the end of the turn (wordingSummary)
		saveTuneResult(res)
		appendRunNote(res.RunPage, fmt.Sprintf("**Not adopted:** %s", strings.TrimPrefix(res.AdoptError, "not adopted: ")))
		return json.NewEncoder(stdout).Encode(map[string]any{"adopted": false, "verdict": req.Verdict, "reason": err.Error()})
	}
	res.AutoAdopted = true
	saveTuneResult(res)
	told := "the person is told"
	if lang != "" {
		told += " with translations"
	}
	appendRunNote(res.RunPage, fmt.Sprintf("**Adopted** as %s after the agent checked the meaning; %s and can undo it on the page.", path, told))
	return json.NewEncoder(stdout).Encode(map[string]any{"adopted": true, "verdict": req.Verdict, "path": path, "next": "the person is told (it can be undone on the page)"})
}

// hookSpecPath is the file of the current version of the project's question id (0.9.2). Adopting a wording adds <id>-v<N>.yaml, so
// the file with the highest version for the same id is used (chosen like serve's loadSpecs). Undone versions (.reverted) do not end in .yaml and are not looked at.
// Unreadable files are skipped; if none can be read, the original <id>.yaml is returned (the caller's Load reports the error).
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
