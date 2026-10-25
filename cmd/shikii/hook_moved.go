package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/hayaenfj/shikii/internal/notebook"
)

// フォルダを移した・名前を変えた後の知らせ(0.9.2)。置き場の名と初期化の印はプロジェクトのパスで決まるので、
// 移しただけだと hook が黙って止まり、印だけ直すと wiki が黙って空になっていた。直し方は作法の references/wiki.md。

const movedHow = "作法の references/wiki.md の「Moving or renaming the project folder」の手順で直してください(人に確かめてから。古い置き場は消さずに写す)。"

// movedMarkerNotice は、このフォルダの初期化の印が前のパスのままで、そのパスがもう無い(フォルダを移した)時の知らせ。
// 前のパスが有る(写した)時と、読めない時は黙る(写した印は有効にしない、という今までの決まりのまま)。
func movedMarkerNotice(cwd string) string {
	root, scope, err := projectStoreAt(cwd)
	if err != nil {
		return ""
	}
	f, err := os.OpenFile(filepath.Join(root, "project.json"), os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return ""
	}
	defer f.Close()
	var a projectActivation
	if json.NewDecoder(io.LimitReader(f, 4096)).Decode(&a) != nil || a.Scope == "" || a.Scope == scope || !filepath.IsAbs(a.Scope) {
		return ""
	}
	if _, err := os.Lstat(a.Scope); !os.IsNotExist(err) {
		return ""
	}
	return fmt.Sprintf("shikii: このフォルダの .shikii/project.json は前のパス(%s)で初期化した印のままなので、shikii は動いていません(教訓の配送・判定・記録が止まっています)。フォルダを移したか名前を変えた後です。今回の依頼を終えたら、%s", a.Scope, movedHow)
}

// otherPathNotice は、このプロジェクトの置き場に、前のパスの名のまま残った wiki の置き場がある時の知らせ。
func otherPathNotice(agent, scope string) string {
	stores, err := (notebook.Book{Root: agent, Scope: scope}).OtherPathStores()
	if err != nil || len(stores) == 0 {
		return ""
	}
	return fmt.Sprintf("shikii: .shikii に、前のパスの名のまま残った wiki の置き場があります(%s)。フォルダを移したか名前を変えた後で、その記事と原資料は今の wiki に出てきません。今回の依頼を終えたら、%s", strings.Join(stores, "、"), movedHow)
}
