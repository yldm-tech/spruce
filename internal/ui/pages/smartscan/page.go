// Package smartscan is the 智能扫描 page.
package smartscan

import (
	"github.com/nsgo-dev/nsgo/core"

	"github.com/yldm-tech/spruce/internal/ui/kit"
	"github.com/yldm-tech/spruce/internal/ui/nav"
)

// Page is the 智能扫描 page.
func Page(cx *core.Scope) core.Node {
	return kit.Page{Children: []core.Node{kit.PageHeader{Route: nav.SmartScan}}}
}
