package main

func recordWorkPlan(root, scope string, token workToken, w *workState, req workRequest, save func() error) (workItem, any, error) {
	item := workItem{Ref: "plan", Kind: "plan"}
	p := planningRequest{SessionID: token.Session, TurnID: token.Turn, Query: w.Query, Pages: w.Pages, ContinuesPrevious: req.ContinuesPrevious, workNonce: token.Nonce}
	var err error
	p.Agreed, err = resolveWorkFact(req.Agreed, w)
	if err != nil {
		return item, nil, err
	}
	p.Open, err = resolveWorkFact(req.Open, w)
	if err != nil {
		return item, nil, err
	}
	key := "plan:" + workHash(p)
	if prior, ok := w.Saved[key]; ok {
		return prior, w.Replies[key].Plan, nil
	}
	if e := workSavedCapacity(w, key); e != nil {
		return item, nil, e
	}
	value, err := runPlanning(root, scope, "submit", p)
	if err != nil {
		return item, nil, err
	}
	receipt := value.(planningReceipt)
	w.Replies[key] = workReply{Plan: &receipt}
	w.Saved[key] = item
	return item, value, save()
}
