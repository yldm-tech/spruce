import { isWindows } from "../../platform";
import { useCleaner } from "../common/cleaner";
import { CleanerView } from "../common/CleanerView";
import { truncateMiddle, trashName } from "../common/format";
import { lastCleanText, useLastClean } from "../common/lastClean";
import { useSettings } from "../common/settings";

/** What the scan looks for, as the Go engine (internal/engine/installers) classifies files. */
function scanDescription(): string {
  return isWindows()
    ? "在下面的文件夹和它们的下一层子文件夹里查找 .exe、.msi、.msix、.appx 安装程序和 .iso 映像。"
    : "在下面的文件夹和它们的下一层子文件夹里查找 .dmg、.pkg、.iso、.xip 和像安装包的 .zip 文件。";
}

/** InstallersPage is the 安装包 page: it finds installers in the folders set in 设置 and moves the ones the user checks to the Trash. */
export function InstallersPage() {
  const flow = useCleaner("installers");
  const last = useLastClean("installers");
  const settings = useSettings();
  const roots = settings.data?.cleanup.installerRoots ?? [];
  const trashLine = `清理的安装包会移到${trashName()}，可以随时放回。`;
  return (
    <CleanerView
      page="installers"
      flow={flow}
      idleTitle="查找安装包"
      idleDescription={scanDescription()}
      idleDetails={
        <>
          {settings.isSuccess &&
            (roots.length > 0 ? (
              <ul aria-label="查找的文件夹">
                {roots.map((r) => (
                  <li key={r} title={r}>
                    {truncateMiddle(r, 64)}
                  </li>
                ))}
              </ul>
            ) : (
              <span>还没有要查找的文件夹，可以在「设置」里添加。</span>
            ))}
          <span>{trashLine}</span>
          {last.data && <span>{lastCleanText(last.data)}</span>}
        </>
      }
      scanDisabled={settings.isSuccess && roots.length === 0}
      listLabel="找到的安装包"
      notice={`${trashLine}${isWindows() ? "最近 7 天内下载的默认不选。" : "最近 7 天内下载的和仍在挂载的默认不选。"}`}
      sizeLabel="已选大小"
      emptyTitle="没有找到安装包"
      emptyDescription="这些文件夹里没有旧的安装包或磁盘映像。"
    />
  );
}
