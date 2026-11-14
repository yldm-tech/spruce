// Package space is the 空间透视 page.
package space

import (
	"github.com/nsgo-dev/nsgo/core"

	"github.com/yldm-tech/spruce/internal/ui/kit"
	"github.com/yldm-tech/spruce/internal/ui/nav"
)

// Page is the 空间透视 page.
func Page(cx *core.Scope) core.Node {
	return kit.Page{Children: []core.Node{kit.PageHeader{Route: nav.Space}}}
}
