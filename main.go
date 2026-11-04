package main

import (
	"log"

	"github.com/egoist/mygo"
	mygoUI "github.com/egoist/mygo/ui"

	_ "db-g/driver/mysql"
	_ "db-g/driver/sqlite"
	"db-g/ui"
)

func main() {
	r := ui.NewRoot()
	mygo.App.WhenReady(func() {
		win := mygo.NewWindow(mygo.WindowOptions{
			Title:   "db-g",
			Width:   1280,
			Height:  800,
			Content: mygoUI.View(r.View),
		})
		r.Attach(win)
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
	r.FlushAll()
}
