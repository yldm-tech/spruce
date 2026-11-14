import { isWindows } from "../../platform";
import { useCleaner } from "../common/cleaner";
import { CleanerView } from "../common/CleanerView";
import { lastCleanText, useLastClean } from "../common/lastClean";

/** What the scan looks at, as the Go engine (internal/engine/systemjunk) lists its categories. */
function scanDescription(): string {
  return isWindows()
    ? "检查临时文件、应用缓存、浏览器缓存、错误报告、未完成的下载和回收站。只看你自己的用户文件夹，不需要管理员权限。"
    : "检查应用缓存、浏览器缓存、日志、崩溃报告、未完成的下载和废纸篓。只看你自己的用户文件夹，不需要管理员权限。";
}

/** SystemJunkPage is the 系统垃圾 page: it scans with the system-junk cleaner and cleans what the user checks. */
export function SystemJunkPage() {
  const flow = useCleaner("system-junk");
  const last = useLastClean("systemJunk");
  return (
    <CleanerView
      page="system-junk"
      flow={flow}
      idleTitle="扫描系统垃圾"
      idleDescription={scanDescription()}
      idleDetails={last.data ? <span>{lastCleanText(last.data)}</span> : undefined}
      listLabel="可以清理的系统垃圾"
      sizeLabel="可释放"
      emptyTitle="没有找到系统垃圾"
      emptyDescription="缓存、日志和临时文件都很干净，过段时间再来看看。"
    />
  );
}
