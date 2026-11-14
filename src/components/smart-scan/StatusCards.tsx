import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import type { ReactNode } from "react";

import { Permissions, type Run, type SpaceVolume } from "../../mygo";
import { isWindows } from "../../platform";
import { formatBytes, formatRelativeTime, trashName } from "../common/format";
import { IconTile, type Tone } from "../common/Glyph";
import { PAGES } from "../common/pages";
import { useSettings } from "../common/settings";
import { historyQueryOptions } from "../history/queries";
import { useHealthAlerts } from "../maintenance/AlertBanner";
import { fullDiskAccessQueryOptions } from "../maintenance/queries";
import { formatAlertTitle } from "../maintenance/summary";
import { locationsQueryOptions } from "../space/queries";

/** A maintenance run older than this many days earns a 「去维护」. */
export const MAINTENANCE_DUE_DAYS = 30;

/** Below this share of free space, or below LOW_FREE_BYTES, the disk card warns. */
const LOW_FREE_SHARE = 0.1;
const LOW_FREE_BYTES = 20e9;

/** diskLevel says whether a volume is running out of space. */
export function diskLevel(volume: Pick<SpaceVolume, "free" | "total">): "ok" | "low" {
  if (volume.total <= 0) return "ok";
  return volume.free / volume.total < LOW_FREE_SHARE || volume.free < LOW_FREE_BYTES ? "low" : "ok";
}

/** lastMaintenance is the newest 维护 run among runs (newest first), or null. */
export function lastMaintenance(runs: readonly Run[] | undefined): Run | null {
  return runs?.find((r) => r.kind === "maintenance") ?? null;
}

/** maintenanceDue says whether 维护 should be offered: it never ran, or its last run is older than MAINTENANCE_DUE_DAYS. */
export function maintenanceDue(run: Run | null, now: Date = new Date()): boolean {
  if (!run) return true;
  const ended = new Date(run.endedAt).getTime();
  return !Number.isFinite(ended) || now.getTime() - ended > MAINTENANCE_DUE_DAYS * 86400e3;
}

interface StatusCardProps {
  title: string;
  tone: Tone;
  icon: ReactNode;
  /** The state in a few words, e.g. "可用 120 GB". */
  value: string;
  /** Shown in the warning colour. */
  warning?: boolean;
  /** A line under the value. */
  note?: string;
  children?: ReactNode;
}

function StatusCard({ title, tone, icon, value, warning, note, children }: StatusCardProps) {
  return (
    <section aria-label={title} className="smart-status-card">
      <div className="smart-status-head">
        <IconTile tone={tone} size={32} glyphSize={17} className="smart-status-icon">
          {icon}
        </IconTile>
        <span className="smart-status-title">{title}</span>
      </div>
      <span className={warning ? "smart-status-value smart-status-value--warning" : "smart-status-value"}>
        {value}
      </span>
      {note && <span className="smart-status-note">{note}</span>}
      {children && <div className="smart-status-actions">{children}</div>}
    </section>
  );
}

function DiskCard() {
  const locations = useQuery(locationsQueryOptions);
  const volume = locations.data?.volumes.find((v) => v.startup) ?? locations.data?.volumes[0];
  let value = "正在读取…";
  let note: string | undefined;
  let low = false;
  if (volume) {
    low = diskLevel(volume) === "low";
    value = `可用 ${formatBytes(volume.free)}`;
    note = `${volume.name || volume.path} 共 ${formatBytes(volume.total)}${low ? "，空间不多了" : ""}`;
  } else if (locations.isSuccess || locations.isError) {
    value = "无法读取";
  }
  const used = volume && volume.total > 0 ? Math.min(1, Math.max(0, 1 - volume.free / volume.total)) : 0;
  return (
    <StatusCard
      title="磁盘空间"
      tone={PAGES.space.tone}
      icon={PAGES.space.icon}
      value={value}
      warning={low}
      note={note}
    >
      {volume && (
        <div
          role="meter"
          aria-label="已用空间"
          aria-valuemin={0}
          aria-valuemax={100}
          aria-valuenow={Math.round(used * 100)}
          className="smart-status-meter"
        >
          <span
            className={
              low ? "smart-status-meter-fill smart-status-meter-fill--low" : "smart-status-meter-fill"
            }
            style={{ width: `${used * 100}%` }}
          />
        </div>
      )}
      <Link to="/space" className="link-button">
        {low ? "看看是什么占用了空间" : "空间透视"}
      </Link>
    </StatusCard>
  );
}

function CpuCard() {
  const alerts = useHealthAlerts();
  const settings = useSettings();
  const enabled = settings.data?.health.enabled ?? true;
  const top = alerts[0];
  let value = "没有长时间高占用的进程";
  let note: string | undefined = "Spruce 只提醒，不会结束进程";
  if (!enabled) {
    value = "高 CPU 提醒已关闭";
    note = "可以在「设置」里重新打开";
  } else if (top) {
    value = formatAlertTitle(top);
    note =
      alerts.length > 1
        ? `另有 ${alerts.length - 1} 个进程也在高占用`
        : "如果不是你正在用它，可以看看是什么在运行";
  }
  return (
    <StatusCard
      title="CPU 占用"
      tone={PAGES.health.tone}
      icon={PAGES.health.icon}
      value={value}
      warning={enabled && !!top}
      note={note}
    >
      {enabled ? (
        <Link to="/health" className="link-button">
          健康监测
        </Link>
      ) : (
        <Link to="/settings" className="link-button">
          去设置
        </Link>
      )}
    </StatusCard>
  );
}

/** What Full Disk Access unlocks in Spruce, as the engines use it (systemjunk's Trash and Safari caches, the 维护 database task, 空间透视's protected folders). */
function fullDiskAccessNote(): string {
  return `开启后才能读取和清空${trashName()}、清理 Safari 缓存、优化 Safari 和邮件的数据库，空间透视也能算进受保护的文件夹`;
}

function FullDiskAccessCard() {
  const queryClient = useQueryClient();
  const state = useQuery(fullDiskAccessQueryOptions);
  const guide = useMutation({
    mutationFn: () => Permissions.guideFullDiskAccess(),
    onSuccess: (next) => queryClient.setQueryData(fullDiskAccessQueryOptions.queryKey, next),
  });
  if (state.data === "notRequired") return null;
  const granted = state.data === "granted";
  let value = "正在检查…";
  if (granted) value = "已开启";
  else if (state.data === "denied") value = "未开启";
  else if (state.data === "unknown" || state.isError) value = "无法确定";
  return (
    <StatusCard
      title="完全磁盘访问权限"
      tone="amber"
      icon={
        <>
          <rect x="5" y="11" width="14" height="9" rx="2" />
          <path d="M8 11V8a4 4 0 0 1 8 0v3" />
        </>
      }
      value={value}
      warning={state.data === "denied"}
      note={granted ? "废纸篓、Safari 缓存和受保护的文件夹都能扫描" : fullDiskAccessNote()}
    >
      {state.data !== undefined && !granted && (
        <button
          type="button"
          className="ghost-button"
          disabled={guide.isPending}
          onClick={() => guide.mutate()}
        >
          {guide.isPending ? "等待授权…" : "去授权"}
        </button>
      )}
      {guide.isError && (
        <span role="alert" className="smart-status-error">
          无法打开系统设置
        </span>
      )}
    </StatusCard>
  );
}

function maintenanceNote(loaded: boolean, run: Run | null, due: boolean): string | undefined {
  if (!loaded) return undefined;
  if (!run) return "做一次轻量保养，不会动你的文档和照片";
  return due ? `已经超过 ${MAINTENANCE_DUE_DAYS} 天没有维护了，可以再做一次` : "最近做过维护";
}

function MaintenanceCard() {
  const history = useQuery(historyQueryOptions);
  const run = lastMaintenance(history.data);
  const due = history.isSuccess && maintenanceDue(run);
  let value = "正在读取…";
  if (history.isSuccess)
    value = run
      ? `上次维护：${formatRelativeTime(run.endedAt) || formatRelativeTime(run.startedAt)}`
      : "还没有做过维护";
  else if (history.isError) value = "无法读取操作历史";
  return (
    <StatusCard
      title="维护"
      tone={PAGES.maintenance.tone}
      icon={PAGES.maintenance.icon}
      value={value}
      warning={due}
      note={maintenanceNote(history.isSuccess, run, due)}
    >
      {due && (
        <Link to="/maintenance" className="link-button">
          去维护
        </Link>
      )}
    </StatusCard>
  );
}

/** StatusCards are the informational cards of 智能扫描: free space on the system disk, the sustained high CPU alert, Full Disk Access (macOS) and when 维护 last ran. They clean nothing; each links to the page that acts on it. */
export function StatusCards() {
  return (
    <section aria-label="其他检查" className="smart-status">
      <h2 className="smart-section-title">其他检查</h2>
      <div className="smart-status-grid">
        <DiskCard />
        <CpuCard />
        {!isWindows() && <FullDiskAccessCard />}
        <MaintenanceCard />
      </div>
    </section>
  );
}
