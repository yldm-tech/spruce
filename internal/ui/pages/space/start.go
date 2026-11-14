package space

import (
	"github.com/nsgo-dev/nsgo/core"

	"github.com/yldm-tech/spruce/internal/app"
	"github.com/yldm-tech/spruce/internal/ui/format"
	"github.com/yldm-tech/spruce/internal/ui/kit"
	"github.com/yldm-tech/spruce/internal/ui/store"
)

// cardHover is a start card's background under the pointer (--space-card-hover).
var cardHover = core.RGBA(0xffffff, 0.13)

// targetGlyphs are the tiles of the quick folders by SpaceTarget.ID; any other folder shows the slate folder.
var targetGlyphs = map[string]struct {
	tone  kit.Tone
	glyph string
}{
	"home":         {kit.ToneViolet, "home"},
	"downloads":    {kit.TonePink, "downloads"},
	"applications": {kit.ToneBlue, "apps"},
	"programFiles": {kit.ToneBlue, "apps"},
}

// cardGrid lays cards out as the design's grid of 250-point columns.
func cardGrid(label string, cards []core.Node) core.Node {
	return core.View{Role: core.RoleGroup, Label: label,
		Style:    core.Style{Display: core.DisplayGrid, GridTemplateColumns: "repeat(auto-fill, minmax(250px, 1fr))", Gap: 14},
		Children: cards}
}

// startView is the page before a scan: the disks with how full they are, the quick folders, and 选择其他文件夹. Choosing any of them starts a scan. The disks are read again each time it shows, since their free space changes outside the app.
func startView(cx *core.Scope, s *State) core.Node {
	locs := store.Use(cx).Locations
	// The first read starts loading them; a later one shows what was read before while they load again.
	if core.Untracked(cx, func() bool { _, ok := locs.Get(); return ok }) {
		locs.Bump()
	}
	return core.View{TestID: "space-start", Busy: func() bool { _, ok := locs.Get(); return !ok }, Style: core.Style{Gap: 22},
		Children: []core.Node{
			kit.ErrorLine{Text: s.Err.Get, TestID: "space-error"},
			core.Dynamic(func(cx *core.Scope) core.Node {
				v, _ := locs.Get()
				var sections []core.Node
				if len(v.Volumes) > 0 {
					cards := make([]core.Node, len(v.Volumes))
					for i, vol := range v.Volumes {
						cards[i] = volumeCard(vol, s)
					}
					sections = append(sections, core.View{Style: core.Style{Gap: 10}, Children: []core.Node{kit.SectionTitle("磁盘"), cardGrid("磁盘", cards)}})
				}
				if len(v.Targets) > 0 {
					cards := make([]core.Node, len(v.Targets))
					for i, t := range v.Targets {
						cards[i] = targetCard(t, s)
					}
					sections = append(sections, core.View{Style: core.Style{Gap: 10}, Children: []core.Node{kit.SectionTitle("常用位置"), cardGrid("常用位置", cards)}})
				}
				return core.Group(sections)
			}),
			core.View{Style: core.Style{Direction: core.Row}, Children: []core.Node{
				kit.Button{Variant: kit.ButtonGhost, TestID: "choose-folder", Disabled: s.Choosing.Get, OnPress: s.Choose,
					TextFunc: func() string {
						if s.Choosing.Get() {
							return "正在选择…"
						}
						return "选择其他文件夹…"
					}},
			}},
		}}
}

// cardStyle is a start card's look (.space-card): the glass card as a button.
func cardStyle() core.Style {
	return core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 14, Padding: core.Symmetric(18, 20), CornerRadius: kit.RadiusCard,
		Background: kit.SurfaceCard, BorderWidth: 1, BorderColor: kit.BorderCard, Color: kit.ColorText, Cursor: core.CursorPointer,
		Transition:   core.Transition{Duration: 150_000_000, Easing: core.Ease, Properties: core.TransitionBackground},
		Hover:        &core.Style{Background: cardHover},
		FocusVisible: &core.Style{OutlineWidth: 2, OutlineOffset: 2, OutlineColor: kit.ColorText}}
}

// cardText is the column beside a card's tile: its title, the line under it and what follows.
func cardText(title, sub string, more ...core.Node) core.Node {
	children := []core.Node{
		kit.Text(title, kit.FontCardTitle, kit.Semibold, kit.ColorText),
		core.Text{Content: sub, MaxLines: 1, EllipsizeMode: core.EllipsizeTail,
			Style: core.Style{FontFamily: kit.FontSans, FontSize: kit.FontBody, FontVariant: core.FontTabularNums, Color: kit.ColorTextMuted}},
	}
	return core.View{Style: core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0), Gap: 4}, Children: append(children, more...)}
}

// volumeCard is a disk: its tile (cyan for the startup disk), name, free and total space, and how full it is, red from 90%.
func volumeCard(v app.SpaceVolume, s *State) core.Node {
	usage := UsageOf(v)
	tone := kit.ToneSlate
	if v.Startup {
		tone = kit.ToneCyan
	}
	fill := kit.ToneCyan
	if usage.Share >= 0.9 {
		fill = kit.ToneRed
	}
	bar := core.View{AccessibilityElementsHidden: func() bool { return true },
		Style: core.Style{Height: core.Pt(6), Margin: core.Edges{Top: 6}, CornerRadius: kit.RadiusPill, Background: kit.ProgressTrack, Overflow: core.Hidden,
			Direction: core.Row},
		Children: []core.Node{core.View{Style: core.Style{Width: core.Pct(float32(usage.Share * 100)), Height: core.Pct(100), CornerRadius: kit.RadiusPill,
			Gradients: []core.Gradient{fill.Gradient()}}}}}
	return core.View{Role: core.RoleButton, Label: v.Name + "，" + usage.Text + "，" + UsedText(usage.Share), TestID: "volume-" + v.Path,
		Style:   cardStyle(),
		OnPress: func(core.PressEvent) { s.Scan(ScanRequest{Path: v.Path, Name: v.Name}) },
		Children: []core.Node{
			kit.IconTile{Tone: tone, Size: 42, GlyphSize: 22, Glyph: "volume", Radius: kit.RadiusCardIcon},
			cardText(v.Name, usage.Text, bar),
		}}
}

// targetCard is a quick folder: its tile, name and path, the whole path in its tooltip.
func targetCard(t app.SpaceTarget, s *State) core.Node {
	g, ok := targetGlyphs[t.ID]
	if !ok {
		g.tone, g.glyph = kit.ToneSlate, "folder"
	}
	return core.View{Role: core.RoleButton, Label: t.Name, Hint: t.Path, TestID: "target-" + t.ID, Style: cardStyle(),
		Tooltip: kit.Tooltip(t.Path),
		OnPress: func(core.PressEvent) { s.Scan(ScanRequest{Path: t.Path, Name: t.Name}) },
		Children: []core.Node{
			kit.IconTile{Tone: g.tone, Size: 42, GlyphSize: 22, Glyph: g.glyph, Radius: kit.RadiusCardIcon},
			cardText(t.Name, format.TruncateMiddle(t.Path, 48)),
		}}
}
