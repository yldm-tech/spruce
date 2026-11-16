package cli

import (
	"fmt"
	"strings"

	"spruce/apps/spruce/internal/ui/format"
)

// generalHelp is spruce help.
const generalHelp = `用法：spruce <命令> [选项]

命令：
  scan             检查可以清理的内容，列出类别、大小和项数
  recommendations  检查后列出清理建议（也可以写 list）
  clean            按建议或类别清理；只有加 --yes 才会删除
  projects         列出项目的构建产物和 Git 状态
  history          列出操作历史
  restore          把一次清理移到废纸篓的项目放回原处（macOS）
  help             显示帮助，spruce help <命令> 显示该命令的用法
  version          显示版本

除 clean --yes 与 restore --yes 外，所有命令都不会删除或改动你的文件；history 与 restore 会先补记 Spruce 中途退出的清理。
除 help 外，每个命令都可以加 --json，在标准输出写一个 JSON 对象。

退出码：0 成功（包括没有可做的和演练），1 失败，2 用法错误，3 部分完成，4 Spruce 正在清理，130 被中断。
`

// commandHelp is spruce help <command>, by command.
var commandHelp = map[string]string{
	"scan": `用法：spruce scan [--source <来源>]... [--json]

检查系统垃圾、开发者垃圾和安装包里可以清理的内容，列出每个类别的大小和项数。只读取，不会删除任何东西。

  --source <来源>  只检查这个来源：system-junk、dev-junk 或 installers，可以重复
  --json           输出 JSON
`,
	"recommendations": `用法：spruce recommendations [--json]

检查后列出清理建议：建议 ID、标题、大小、动作和理由。建议 ID 可以交给 spruce clean。只读取，不会删除任何东西。

  --json  输出 JSON
`,
	"clean": `用法：spruce clean [<建议 ID>...] [--category <类别 ID>]... [--source <来源>]... [--older-than <时长>] [--yes | --dry-run] [--json]

检查，按下面的规则选择项目，列出清理计划。只有加 --yes 才会清理，否则只是演练。

  <建议 ID>             清理这条建议的项目（spruce recommendations 可以看到），包括需要运行命令的建议
  --category <类别 ID>  清理这个类别的全部项目（spruce scan 可以看到），可以重复
  --source <来源>       只检查这个来源：system-junk、dev-junk 或 installers，可以重复
  --older-than <时长>   只清理至少这么久没有改动的项目，例如 12h、30d、2w
  --yes                 执行清理
  --dry-run             只列出计划，不清理（不加 --yes 时也是这样）
  --json                输出 JSON

不指定建议和类别时，选择全部不需要运行命令的建议。清理和应用里一样经过全部检查：排除的位置、删除前的再次确认、有未提交改动的项目，这些项目无论怎样指定都不会被清理。清理会记入操作历史。` + restoreHint + `
`,
	"projects": `用法：spruce projects [--json]

检查项目文件夹，列出每个项目的构建产物、大小、最后活动和 Git 状态。只读取，不会删除任何东西。

  --json  输出 JSON
`,
	"history": `用法：spruce history [--limit <数量>] [--json]

列出最近的操作记录，以及哪些还能放回原处。会先补记 Spruce 中途退出的清理，不会改动你的文件。

  --limit <数量>  最多列出多少条，默认 20，最多 500
  --json          输出 JSON
`,
	"restore": `用法：spruce restore <记录 ID> [--yes] [--json]

列出一次清理移到废纸篓、还能放回原处的项目；加 --yes 才会放回。列出前会先补记 Spruce 中途退出的清理。不会覆盖任何东西，原位置已有同名项目的不放回。只在 macOS 上可用。

  <记录 ID>  spruce history 列出的记录
  --yes      执行放回
  --json     输出 JSON
`,
	"version": `用法：spruce version [--json]

显示 Spruce 的版本。
`,
	"help": `用法：spruce help [<命令>]

显示帮助。
`,
}

// restoreHint is the sentence of clean's help about putting moves back, left out where restore is not available (Windows).
const restoreHint = "移到废纸篓的项目可以用 spruce restore 放回原处。"

func (r *run) help(args []string) int {
	if len(args) == 0 {
		fmt.Fprint(r.stdout, r.generalHelp())
		return ExitOK
	}
	name := commands[args[0]]
	if text, ok := commandHelp[name]; ok && len(args) == 1 {
		if !r.d.CanRestore {
			text = strings.ReplaceAll(text, restoreHint, "")
		}
		fmt.Fprint(r.stdout, binNamed(text))
		return ExitOK
	}
	if len(args) > 1 {
		return r.usage(fmt.Errorf("多余的参数 %q", args[1]), "")
	}
	return r.usage(fmt.Errorf("没有命令 %q", strings.TrimSpace(args[0])), "")
}

// generalHelp is spruce help, as the platform names the bin.
func (r *run) generalHelp() string { return binNamed(generalHelp) }

// binNamed writes the help's 废纸篓 as the system calls it (回收站 on Windows).
func binNamed(text string) string { return strings.ReplaceAll(text, "废纸篓", format.TrashName()) }
