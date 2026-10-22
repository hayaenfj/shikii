package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
)

// 過去の会話とwikiは判断材料。今の人の発言や現在の合意に置き換えない。
// 直近4件の原文だけを控え、要約の再要約や会話の連結をしない。
type planningTurn struct {
	ID      string    `json:"id"`
	Request string    `json:"request"`
	Summary string    `json:"summary,omitempty"`
	At      time.Time `json:"at"`
}

func addPlanningContext(scope string, previous *hookSession, f *planningFrame) {
	if previous.Scope == scope && previous.Planning != nil && time.Since(previous.Planning.At) >= 0 && time.Since(previous.Planning.At) < hookSessionTTL {
		f.Recent = append([]planningTurn{}, previous.Planning.Recent...)
		// 更新前の形式にも残っている原文だけを参照する。過去の結合されたRequestからは作らない。
		if len(f.Recent) == 0 && previous.Planning.Sources["previous_user"] != "" {
			f.Recent = append(f.Recent, planningTurn{ID: previous.TurnID + "-prior", Request: cut(previous.Planning.Sources["previous_user"], 1200), At: previous.Planning.At})
		}
		if previous.Planning.Sources["current_user"] != "" {
			turn := planningTurn{ID: previous.TurnID, Request: cut(previous.Planning.Sources["current_user"], 1200), At: previous.Planning.At}
			if previous.Completed != nil && previous.Completed.TurnID == previous.TurnID {
				turn.Summary = cut(previous.Completed.Summary, 1200)
			}
			f.Recent = append(f.Recent, turn)
		}
	}
	if len(f.Recent) > 4 {
		f.Recent = f.Recent[len(f.Recent)-4:]
	}
	for _, turn := range f.Recent {
		if time.Since(turn.At) < 0 || time.Since(turn.At) > hookSessionTTL {
			continue
		}
		f.Sources["history:"+turn.ID+":user"] = cut(turn.Request, 1200)
		if turn.Summary != "" {
			f.Sources["history:"+turn.ID+":assistant"] = cut(turn.Summary, 1200)
		}
	}
}

// 検索語はskillを読んだエージェントが選ぶ。プログラムは話題や合意を推測しない。
func planningWikiSources(root, scope, query string, sources map[string]string) string {
	if strings.TrimSpace(query) == "" {
		return ""
	}
	view, err := (notebook.Book{Root: root, Scope: scope}).Query(query)
	if err != nil {
		return "wikiの参照を読み出せません。未知の条件を補わず、現在の依頼を優先してください。"
	}
	for _, page := range view.Lessons {
		key := "lesson:" + page.ID + ":" + page.Revision
		ids := []string{}
		for _, source := range page.Sources {
			ids = append(ids, source.ID)
		}
		sources[key] = fmt.Sprintf("教訓（仮説）: %s\n適用条件: %s\n出典: %s", cut(page.Lesson, 800), cut(page.AppliesTo, 300), strings.Join(ids, ", "))
	}
	if len(view.Issues) > 0 {
		return "出典や版を確認できないwikiは参照から外しました。"
	}
	return ""
}
