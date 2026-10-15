package ui

import (
	"github.com/egoist/mygo/ui"
)

func (a *root) sidebar(c *ui.Context) {
	ui.Column(c).Fill().Padding(8).Gap(6).Children(func() {
		if a.store != nil {
			if w := a.store.Warning(); w != "" {
				ui.Text(c, w).TextColor(c.Theme().Danger)
			}
			if len(a.store.List()) == 0 {
				ui.Text(c, "左上角「新建连接」开始").TextColor(c.Theme().TextMuted)
			}
		} else {
			ui.Text(c, "左上角「新建连接」开始").TextColor(c.Theme().TextMuted)
		}
	})
}

func (a *root) connDot(c *ui.Context) {
	switch {
	case a.con.Session() != nil:
		ui.Text(c, "●").TextColor(c.Theme().Success)
	case a.connErr != "":
		ui.Text(c, "●").TextColor(c.Theme().Danger)
	default:
		ui.Text(c, "●").TextColor(c.Theme().TextMuted)
	}
}
