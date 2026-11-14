// Package uninstall is the 应用卸载 page.
package uninstall

import (
	"github.com/nsgo-dev/nsgo/core"

	"github.com/yldm-tech/spruce/internal/ui/kit"
	"github.com/yldm-tech/spruce/internal/ui/nav"
)

// Page is the 应用卸载 page.
func Page(cx *core.Scope) core.Node {
	return kit.Page{Children: []core.Node{kit.PageHeader{Route: nav.Uninstall}}}
}
