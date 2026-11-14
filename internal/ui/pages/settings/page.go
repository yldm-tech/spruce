// Package settings is the 设置 page.
package settings

import (
	"github.com/nsgo-dev/nsgo/core"

	"github.com/yldm-tech/spruce/internal/ui/kit"
	"github.com/yldm-tech/spruce/internal/ui/nav"
)

// Page is the 设置 page.
func Page(cx *core.Scope) core.Node {
	return kit.Page{Children: []core.Node{kit.PageHeader{Route: nav.Settings}}}
}
