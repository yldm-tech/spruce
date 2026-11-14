package health

import (
	"runtime"
	"strconv"
	"time"

	"github.com/nsgo-dev/nsgo/core"

	"github.com/yldm-tech/spruce/internal/engine/sysinfo"
	"github.com/yldm-tech/spruce/internal/ui/kit"
)

// The colors of the page that tokens.css keeps for it alone.
var (
	coreBarTrack     = core.RGBA(0xffffff, 0.1)
	coreBarFill      = kit.ColorAccentBright
	pressureNormal   = core.RGB(0x4be3a0)
	pressureWarning  = kit.ColorWarning
	pressureCritical = kit.ColorDanger
)

// cardState is what a VitalCard shows: 正在读取… before the first sample, 无法读取 when its metric could not be read, else the figures.
type cardState uint8

const (
	cardPending cardState = iota
	cardUnreadable
	cardReady
)

// stateOf is a metric's cardState from the latest sample and whether the metric is in it.
func stateOf(latest *sysinfo.Vitals, present bool) cardState {
	switch {
	case latest == nil:
		return cardPending
	case !present:
		return cardUnreadable
	}
	return cardReady
}

// vitalCard is one overview card of the page: a gradient tile and title, the headline figure, its caption and the details below. The body changes only when State does, so the meters and charts in it stay mounted from sample to sample.
type vitalCard struct {
	Title   string
	Tone    kit.Tone
	Glyph   string
	TestID  string
	State   func() cardState
	Value   func() string
	Caption func() string
	Body    func(cx *core.Scope) []core.Node
}

// Render implements core.Node.
func (c vitalCard) Render(cx *core.Scope) core.Node {
	state := core.NewMemo(cx, c.State)
	head := core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 12}, Children: []core.Node{
		kit.IconTile{Tone: c.Tone, Size: 36, GlyphSize: 18, Glyph: c.Glyph, Radius: kit.RadiusCardIcon},
		core.Text{Content: c.Title, Role: core.RoleHeader, Style: core.Style{FontFamily: kit.FontFace(kit.Semibold), FontWeight: kit.FaceWeight(kit.Semibold),
			FontSize: kit.FontCardTitle, Color: kit.ColorText}},
	}}
	body := core.Dynamic(func(*core.Scope) core.Node {
		switch state.Get() {
		case cardPending:
			return pendingValue("正在读取…")
		case cardUnreadable:
			return pendingValue("无法读取")
		}
		return core.Func(func(cx *core.Scope) core.Node {
			nodes := []core.Node{kit.Tabular(core.Text{ContentFunc: c.Value, Style: core.Style{FontFamily: kit.FontFace(kit.Bold),
				FontWeight: kit.FaceWeight(kit.Bold), FontSize: kit.FontStat, LetterSpacingEm: kit.LetterSpacingStat, Color: kit.ColorText}})}
			if c.Caption != nil {
				nodes = append(nodes, core.Show{When: func() bool { return c.Caption() != "" }, Then: func(*core.Scope) core.Node {
					return core.View{Style: core.Style{Margin: core.Edges{Top: -6}}, Children: []core.Node{
						kit.Tabular(kit.TextFunc(c.Caption, kit.FontBody, kit.Regular, kit.ColorTextSecondary)),
					}}
				}})
			}
			if c.Body != nil {
				nodes = append(nodes, c.Body(cx)...)
			}
			return core.Group(nodes)
		})
	})
	return core.View{Role: core.RoleRegion, Label: c.Title, TestID: c.TestID, Style: core.Style{MinWidth: core.Pt(0)}, Children: []core.Node{
		kit.Card{Padding: core.Symmetric(20, 22), Gap: 10, Style: core.Style{FlexGrow: 1, MinWidth: core.Pt(0)}, Children: []core.Node{head, body}},
	}}
}

// pendingValue is the headline while there is no figure.
func pendingValue(s string) core.Node {
	return kit.Text(s, kit.FontCardTitle, kit.Medium, kit.ColorTextMuted)
}

// detailRow is one label and value pair under a card's headline; Color colors the value (the text color when 0).
type detailRow struct {
	Label string
	Value func() string
	Color func() core.Color
}

// detailList shows label and value pairs under a card's headline.
func detailList(rows ...core.Node) core.Node {
	return core.View{Style: core.Style{Gap: 4}, Children: rows}
}

// Render implements core.Node.
func (r detailRow) Render(*core.Scope) core.Node {
	value := core.Text{ContentFunc: r.Value, StyleFunc: func() core.Style {
		c := kit.ColorText
		if r.Color != nil {
			c = r.Color()
		}
		return core.Style{FontFamily: kit.FontSans, FontSize: kit.FontBody, Color: c, TextAlign: core.TextAlignRight,
			FontVariant: core.FontTabularNums, FlexShrink: 1, MinWidth: core.Pt(0)}
	}}
	return core.View{Style: core.Style{Direction: core.Row, JustifyContent: core.JustifySpaceBetween, Gap: 12}, Children: []core.Node{
		core.Text{Content: r.Label, Style: core.Style{FontFamily: kit.FontSans, FontSize: kit.FontBody, Color: kit.ColorTextMuted, FlexShrink: 0}},
		value,
	}}
}

// foot is a card's quiet last line (.vital-card-foot).
func foot(fn func() string) core.Node {
	t := kit.Tabular(kit.TextFunc(fn, kit.FontBody, kit.Regular, kit.ColorTextMuted))
	return t
}

// legendItem is a series' color swatch and its text.
type legendItem struct {
	Color core.Color
	Text  func() string
}

// legend names a chart's series under it.
func legend(items ...legendItem) core.Node {
	var children []core.Node
	for _, it := range items {
		children = append(children, core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 6}, Children: []core.Node{
			core.View{Style: core.Style{Width: core.Pt(10), Height: core.Pt(3), CornerRadius: 2, Background: it.Color, FlexShrink: 0}},
			kit.Tabular(kit.TextFunc(it.Text, kit.FontBody, kit.Regular, kit.ColorTextSecondary)),
		}})
	}
	return core.View{Style: core.Style{Direction: core.Row, Wrap: core.Wrap, RowGap: 4, ColumnGap: 16}, Children: children}
}

// meter is the page's thin bar for a share of a whole, percent from 0 to 100.
func meter(label string, percent func() float64, level func() Level, testID string) core.Node {
	return kit.Meter{Height: 6, Label: label, TestID: testID,
		Value: func() float64 { return percent() / 100 },
		Level: func() kit.Level {
			switch level() {
			case LevelCritical:
				return kit.LevelCritical
			case LevelWarning:
				return kit.LevelWarning
			}
			return kit.LevelNormal
		}}
}

// coreBars are the CPU card's bars, one per logical CPU, filled to its use.
func coreBars(perCore func() []float64) core.Node {
	indices := func() []int {
		out := make([]int, len(perCore()))
		for i := range out {
			out[i] = i
		}
		return out
	}
	return core.View{Role: core.RoleImage, LabelFunc: func() string { return CoresLabel(perCore()) }, TestID: "core-bars",
		Style: core.Style{Direction: core.Row, AlignItems: core.AlignEnd, Gap: 3, Height: core.Pt(28)},
		Children: []core.Node{core.Each(indices, func(i int) int { return i }, func(_ *core.Scope, index func() int, _ func() int) core.Node {
			fill := core.View{StyleFunc: func() core.Style {
				p := 0.0
				if cores := perCore(); index() < len(cores) {
					p = min(100, max(0, cores[index()]))
				}
				return core.Style{Height: core.Pct(float32(p)), Background: coreBarFill,
					Transition: core.Transition{Duration: 300 * time.Millisecond, Easing: core.Ease, Properties: core.TransitionLayout}}
			}}
			return core.View{Style: core.Style{FlexGrow: 1, FlexShrink: 1, FlexBasis: core.Pt(0), Height: core.Pct(100), JustifyContent: core.JustifyEnd,
				CornerRadius: 2, Background: coreBarTrack, Overflow: core.Hidden}, Children: []core.Node{fill}}
		})}}
}

// cards are the page's overview cards.
type cards struct {
	s      *State
	cpu    *core.Memo[Chart]
	memory *core.Memo[Chart]
	disk   *core.Memo[Chart]
	net    *core.Memo[Chart]
}

func newCards(cx *core.Scope, s *State) cards {
	chart := func(pickers ...Picker) *core.Memo[Chart] {
		return core.NewMemoFunc(cx, func() Chart { return ChartData(s.Samples.Get(), pickers...) }, nil)
	}
	return cards{s: s,
		cpu: chart(func(v sysinfo.Vitals) (float64, bool) {
			if v.CPU == nil {
				return 0, false
			}
			return v.CPU.Percent, true
		}),
		memory: chart(func(v sysinfo.Vitals) (float64, bool) {
			if v.Memory == nil {
				return 0, false
			}
			return v.Memory.Percent, true
		}),
		disk: chart(func(v sysinfo.Vitals) (float64, bool) {
			if v.DiskIO == nil {
				return 0, false
			}
			return v.DiskIO.ReadPerSec, true
		}, func(v sysinfo.Vitals) (float64, bool) {
			if v.DiskIO == nil {
				return 0, false
			}
			return v.DiskIO.WritePerSec, true
		}),
		net: chart(func(v sysinfo.Vitals) (float64, bool) {
			if v.Network == nil {
				return 0, false
			}
			return v.Network.DownloadPerSec, true
		}, func(v sysinfo.Vitals) (float64, bool) {
			if v.Network == nil {
				return 0, false
			}
			return v.Network.UploadPerSec, true
		}),
	}
}

// The latest sample's metrics, zero while there is none, so a body that is about to go reads safely.
func (c cards) cpuStats() sysinfo.CPUStats {
	if l := c.s.Latest(); l != nil && l.CPU != nil {
		return *l.CPU
	}
	return sysinfo.CPUStats{}
}

func (c cards) memoryStats() sysinfo.MemoryStats {
	if l := c.s.Latest(); l != nil && l.Memory != nil {
		return *l.Memory
	}
	return sysinfo.MemoryStats{}
}

func (c cards) diskUsage() sysinfo.DiskUsage {
	if l := c.s.Latest(); l != nil && l.Disk != nil {
		return *l.Disk
	}
	return sysinfo.DiskUsage{}
}

func (c cards) diskIO() *sysinfo.DiskIO {
	if l := c.s.Latest(); l != nil {
		return l.DiskIO
	}
	return nil
}

func (c cards) network() sysinfo.NetworkIO {
	if l := c.s.Latest(); l != nil && l.Network != nil {
		return *l.Network
	}
	return sysinfo.NetworkIO{}
}

func (c cards) battery() sysinfo.BatteryStats {
	if l := c.s.Latest(); l != nil && l.Battery != nil {
		return *l.Battery
	}
	return sysinfo.BatteryStats{}
}

func (c cards) info() sysinfo.SystemInfo {
	if i := c.s.Info.Get(); i != nil {
		return *i
	}
	return sysinfo.SystemInfo{}
}

func (c cards) CPU() core.Node {
	return vitalCard{Title: "CPU", Tone: kit.ToneAmber, Glyph: "cpu", TestID: "card-cpu",
		State: func() cardState { l := c.s.Latest(); return stateOf(l, l != nil && l.CPU != nil) },
		Value: func() string { return FormatPercent(c.cpuStats().Percent) },
		Caption: func() string {
			if load := c.cpuStats().Load; load != nil {
				return "负载 " + FormatLoad(*load)
			}
			return ""
		},
		Body: func(*core.Scope) []core.Node {
			return []core.Node{
				coreBars(func() []float64 { return c.cpuStats().PerCore }),
				Sparkline{Chart: c.cpu.Get, Series: cpuSeries, Max: 100, TestID: "chart-cpu"},
				core.Show{When: func() bool { return CPUFoot(c.info()) != "" }, Then: func(*core.Scope) core.Node {
					return foot(func() string { return CPUFoot(c.info()) })
				}},
			}
		}}
}

func (c cards) Memory() core.Node {
	return vitalCard{Title: "内存", Tone: kit.ToneViolet, Glyph: "memory", TestID: "card-memory",
		State: func() cardState { l := c.s.Latest(); return stateOf(l, l != nil && l.Memory != nil) },
		Value: func() string { return FormatPercent(c.memoryStats().Percent) },
		Caption: func() string {
			m := c.memoryStats()
			return "已用 " + FormatBytes(float64(m.Used)) + "，共 " + FormatBytes(float64(m.Total))
		},
		Body: func(*core.Scope) []core.Node {
			swap := detailRow{Label: SwapLabel(runtime.GOOS), Value: func() string {
				if sw := c.memoryStats().Swap; sw != nil {
					return "已用 " + FormatBytes(float64(sw.Used))
				}
				return "无法读取"
			}}
			pressure := core.Show{When: func() bool { return c.memoryStats().Pressure != "" }, Then: func(*core.Scope) core.Node {
				return detailRow{Label: "内存压力", Value: func() string { return PressureText(c.memoryStats().Pressure) }, Color: func() core.Color {
					switch PressureLevel(c.memoryStats().Pressure) {
					case LevelCritical:
						return pressureCritical
					case LevelWarning:
						return pressureWarning
					}
					return pressureNormal
				}}
			}}
			return []core.Node{
				meter("内存已用", func() float64 { return c.memoryStats().Percent }, func() Level { return PressureLevel(c.memoryStats().Pressure) }, "meter-memory"),
				Sparkline{Chart: c.memory.Get, Series: memorySeries, Max: 100, TestID: "chart-memory"},
				detailList(swap, pressure),
			}
		}}
}

func (c cards) Disk() core.Node {
	return vitalCard{Title: "磁盘", Tone: kit.ToneCyan, Glyph: "disk", TestID: "card-disk",
		State: func() cardState { l := c.s.Latest(); return stateOf(l, l != nil && l.Disk != nil) },
		Value: func() string { return FormatBytes(float64(c.diskUsage().Free)) + " 可用" },
		Caption: func() string {
			d := c.diskUsage()
			return "已用 " + FormatBytes(float64(d.Used)) + "，共 " + FormatBytes(float64(d.Total))
		},
		Body: func(*core.Scope) []core.Node {
			used := func() float64 { return DiskUsedPercent(c.diskUsage()) }
			return []core.Node{
				meter("系统磁盘已用", used, func() Level { return UsageLevel(used()) }, "meter-disk"),
				Sparkline{Chart: c.disk.Get, Series: diskSeries, MinTop: minRateTop, TestID: "chart-disk"},
				core.Show{When: func() bool { return c.diskIO() != nil },
					Then: func(*core.Scope) core.Node {
						rate := func(read bool) string {
							io := c.diskIO()
							if io == nil {
								return ""
							}
							if read {
								return "读取 " + FormatRate(io.ReadPerSec)
							}
							return "写入 " + FormatRate(io.WritePerSec)
						}
						return legend(legendItem{diskReadColor, func() string { return rate(true) }}, legendItem{diskWriteColor, func() string { return rate(false) }})
					},
					Else: func(*core.Scope) core.Node { return foot(func() string { return "读写速度：无法读取" }) }},
			}
		}}
}

func (c cards) Network() core.Node {
	return vitalCard{Title: "网络", Tone: kit.ToneBlue, Glyph: "network", TestID: "card-network",
		State:   func() cardState { l := c.s.Latest(); return stateOf(l, l != nil && l.Network != nil) },
		Value:   func() string { return "↓ " + FormatRate(c.network().DownloadPerSec) },
		Caption: func() string { return "↑ " + FormatRate(c.network().UploadPerSec) },
		Body: func(*core.Scope) []core.Node {
			return []core.Node{
				Sparkline{Chart: c.net.Get, Series: networkSeries, MinTop: minRateTop, TestID: "chart-network"},
				legend(legendItem{netDownColor, func() string { return "下载" }}, legendItem{netUpColor, func() string { return "上传" }}),
			}
		}}
}

// Battery is shown on a machine with a battery, and says 无法读取 when the battery could not be read.
func (c cards) Battery() core.Node {
	return vitalCard{Title: "电池", Tone: kit.ToneGreen, Glyph: "battery", TestID: "card-battery",
		State:   func() cardState { l := c.s.Latest(); return stateOf(l, l != nil && l.Battery != nil) },
		Value:   func() string { return FormatPercent(c.battery().Percent) },
		Caption: func() string { return BatteryCaption(c.battery()) },
		Body: func(*core.Scope) []core.Node {
			return []core.Node{
				meter("电量", func() float64 { return c.battery().Percent }, func() Level { return BatteryLevel(c.battery()) }, "meter-battery"),
				detailList(
					detailRow{Label: "循环次数", Value: func() string {
						if n := c.battery().CycleCount; n != nil {
							return strconv.Itoa(*n) + " 次"
						}
						return "无法读取"
					}},
					detailRow{Label: "最大容量", Value: func() string {
						if p := c.battery().HealthPercent; p != nil {
							return FormatPercent(*p) + "（相对设计容量）"
						}
						return "无法读取"
					}},
				),
			}
		}}
}

func (c cards) System() core.Node {
	orUnknown := func(s string) string {
		if s == "" {
			return "无法读取"
		}
		return s
	}
	return vitalCard{Title: "系统", Tone: kit.ToneSlate, Glyph: "display", TestID: "card-system",
		State: func() cardState {
			if c.s.Info.Get() == nil {
				return cardPending
			}
			return cardReady
		},
		Value: func() string { return orUnknown(OSText(c.info())) },
		Body: func(*core.Scope) []core.Node {
			return []core.Node{detailList(
				detailRow{Label: "版本号", Value: func() string { return orUnknown(c.info().OSBuild) }},
				detailRow{Label: "型号", Value: func() string { return orUnknown(c.info().Model) }},
				detailRow{Label: "已运行", Value: func() string { return UptimeText(c.info(), c.s.Latest()) }},
				detailRow{Label: "启动于", Value: func() string { return orUnknown(FormatBootTime(c.info().BootTime)) }},
			)}
		}}
}
