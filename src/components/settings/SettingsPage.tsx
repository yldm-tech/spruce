import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useId, useState, type ReactNode } from "react";

import { Permissions, type AppSettings, type PermissionState, type SettingsLimits } from "../../mygo";
import { isWindows } from "../../platform";
import { ConfirmDialog } from "../common/ConfirmDialog";
import { useReveal } from "../common/files";
import { fileManagerName, trashName } from "../common/format";
import { IconTile, type Tone } from "../common/Glyph";
import { Page, PageHeader } from "../common/Page";
import { PAGES } from "../common/pages";
import { useResetSettings, useSettings } from "../common/settings";
import { ClearHistoryDialog } from "../history/ClearHistoryDialog";
import { fullDiskAccessQueryOptions } from "../maintenance/queries";
import {
  FolderListSetting,
  NumberSetting,
  SettingRow,
  SliderSetting,
  SwitchSetting,
  useRowIds,
} from "./controls";
import { LICENSES } from "./licenses";
import { aboutQueryOptions, errorMessage, limitsQueryOptions, type FieldSpec } from "./useSetting";

import "./settings.css";

// ---- Fields ----

function general<K extends keyof AppSettings["general"]>(key: K): FieldSpec<AppSettings["general"][K]> {
  return { get: (s) => s.general[key], put: (s, v) => ({ ...s, general: { ...s.general, [key]: v } }) };
}

function health<K extends keyof AppSettings["health"]>(key: K): FieldSpec<AppSettings["health"][K]> {
  return { get: (s) => s.health[key], put: (s, v) => ({ ...s, health: { ...s.health, [key]: v } }) };
}

function cleanup<K extends keyof AppSettings["cleanup"]>(key: K): FieldSpec<AppSettings["cleanup"][K]> {
  return { get: (s) => s.cleanup[key], put: (s, v) => ({ ...s, cleanup: { ...s.cleanup, [key]: v } }) };
}

const FIELDS = {
  launchAtLogin: general("launchAtLogin"),
  confirmBeforeClean: general("confirmBeforeClean"),
  healthEnabled: health("enabled"),
  cpuThreshold: health("cpuThreshold"),
  sustainMinutes: health("sustainMinutes"),
  exclusions: cleanup("exclusions"),
  devProjectRoots: cleanup("devProjectRoots"),
  devArtifactMinAgeDays: cleanup("devArtifactMinAgeDays"),
  installerRoots: cleanup("installerRoots"),
  largeFileMinMB: cleanup("largeFileMinMB"),
  retentionDays: {
    get: (s) => s.history.retentionDays,
    put: (s, v) => ({ ...s, history: { ...s.history, retentionDays: v } }),
  } satisfies FieldSpec<number>,
};

// ---- Page ----

/** SettingsPage is the 设置 page: one glass card per area (通用, 健康监测, 清理, 历史记录, 权限 on macOS, 关于) and 恢复默认设置. Every control saves on its own as it changes; there is no Save button. */
export function SettingsPage() {
  const settings = useSettings();
  const limits = useQuery(limitsQueryOptions);
  const [confirmReset, setConfirmReset] = useState(false);
  const [clearing, setClearing] = useState(false);
  // Bumped by 恢复默认设置 so every control drops its notes and drafts.
  const [generation, setGeneration] = useState(0);
  const failed = settings.isError ? settings.error : limits.isError ? limits.error : null;

  return (
    <>
      <Page>
        <PageHeader {...PAGES.settings} />
        {failed && (
          <div role="alert" className="settings-load-error">
            <span>{`无法读取设置：${errorMessage(failed)}`}</span>
            <button
              type="button"
              className="ghost-button"
              onClick={() => {
                void settings.refetch();
                void limits.refetch();
              }}
            >
              重试
            </button>
          </div>
        )}
        {settings.data && limits.data && (
          <div key={generation} className="settings-sections">
            <GeneralSection settings={settings.data} />
            <HealthSection settings={settings.data} limits={limits.data} />
            <CleanupSection settings={settings.data} limits={limits.data} />
            <HistorySection settings={settings.data} limits={limits.data} onClear={() => setClearing(true)} />
            <PermissionsSection />
            <AboutSection />
            <div className="settings-reset">
              <button type="button" className="ghost-button" onClick={() => setConfirmReset(true)}>
                恢复默认设置
              </button>
            </div>
          </div>
        )}
      </Page>
      {confirmReset && (
        <ResetDialog
          onDone={() => {
            setConfirmReset(false);
            setGeneration((n) => n + 1);
          }}
          onCancel={() => setConfirmReset(false)}
        />
      )}
      {clearing && <ClearHistoryDialog onClose={() => setClearing(false)} />}
    </>
  );
}

function Section({
  title,
  tone,
  icon,
  children,
}: {
  title: string;
  tone: Tone;
  icon: ReactNode;
  children: ReactNode;
}) {
  const headingId = useId();
  return (
    <section aria-labelledby={headingId} className="settings-card">
      <div className="settings-card-head">
        <IconTile tone={tone} size={32} glyphSize={17} className="category-icon">
          {icon}
        </IconTile>
        <h2 id={headingId}>{title}</h2>
      </div>
      {children}
    </section>
  );
}

interface SectionProps {
  settings: AppSettings;
  limits: SettingsLimits;
}

function GeneralSection({ settings }: { settings: AppSettings }) {
  return (
    <Section title="通用" tone="graphite" icon={PAGES.settings.icon}>
      <SwitchSetting
        settings={settings}
        spec={FIELDS.launchAtLogin}
        label="登录时启动 Spruce"
        description="登录系统后自动打开 Spruce。"
      />
      <SwitchSetting
        settings={settings}
        spec={FIELDS.confirmBeforeClean}
        label="清理前确认"
        description="每次清理前先说明要清理多少、能否恢复，确认后才开始。"
      />
    </Section>
  );
}

function HealthSection({ settings, limits }: SectionProps) {
  return (
    <Section title="健康监测" tone={PAGES.health.tone} icon={PAGES.health.icon}>
      <SwitchSetting
        settings={settings}
        spec={FIELDS.healthEnabled}
        label="启用高 CPU 提醒"
        description="有进程长时间占用大量 CPU 时提醒你。只提醒，不会结束进程。"
      />
      <SliderSetting
        settings={settings}
        spec={FIELDS.cpuThreshold}
        label="提醒阈值"
        description="按单个处理器核心计算：100% 表示占满一个核心，200% 表示占满两个核心。"
        range={limits.cpuThreshold}
        step={5}
        format={(v) => `${v}%`}
      />
      <SliderSetting
        settings={settings}
        spec={FIELDS.sustainMinutes}
        label="持续时间"
        description="进程连续超过阈值这么久才提醒。"
        range={limits.sustainMinutes}
        step={1}
        format={(v) => `${v} 分钟`}
      />
    </Section>
  );
}

function CleanupSection({ settings, limits }: SectionProps) {
  return (
    <Section title="清理" tone={PAGES["system-junk"].tone} icon={PAGES["system-junk"].icon}>
      <FolderListSetting
        settings={settings}
        spec={FIELDS.exclusions}
        label="排除的文件夹"
        description="任何清理功能都不会删除这些位置里的内容。"
        pickerTitle="选择要排除的文件夹"
        maxPaths={limits.maxPaths}
      />
      <FolderListSetting
        settings={settings}
        spec={FIELDS.devProjectRoots}
        label="开发项目所在文件夹"
        description="开发者垃圾在这些文件夹里查找项目的构建产物。"
        pickerTitle="选择开发项目所在的文件夹"
        maxPaths={limits.maxPaths}
      />
      <NumberSetting
        settings={settings}
        spec={FIELDS.devArtifactMinAgeDays}
        label="项目构建产物最少闲置天数"
        description="项目超过这么多天没有改动，才会列出它的构建产物。"
        range={limits.devArtifactMinAgeDays}
        unit="天"
      />
      <FolderListSetting
        settings={settings}
        spec={FIELDS.installerRoots}
        label="安装包查找位置"
        description="安装包在这些文件夹里查找安装包和磁盘映像。"
        pickerTitle="选择查找安装包的文件夹"
        maxPaths={limits.maxPaths}
      />
      <NumberSetting
        settings={settings}
        spec={FIELDS.largeFileMinMB}
        label="大文件阈值"
        description="空间透视把不小于这个大小的文件算作大文件。"
        range={limits.largeFileMinMB}
        unit="MB"
      />
    </Section>
  );
}

function HistorySection({ settings, limits, onClear }: SectionProps & { onClear: () => void }) {
  const ids = useRowIds();
  return (
    <Section title="历史记录" tone={PAGES.history.tone} icon={PAGES.history.icon}>
      <NumberSetting
        settings={settings}
        spec={FIELDS.retentionDays}
        label="保留天数"
        description="更早的操作记录会自动删除。"
        range={limits.retentionDays}
        unit="天"
      />
      <SettingRow label="清空历史" description="删除全部操作记录，无法恢复。" {...ids}>
        <button type="button" className="ghost-button" aria-describedby={ids.descriptionId} onClick={onClear}>
          清空历史…
        </button>
      </SettingRow>
    </Section>
  );
}

const PERMISSION_TEXT: Record<PermissionState, string> = {
  granted: "已授权",
  denied: "未授权",
  unknown: "无法确定",
  notRequired: "不需要",
};

/** PermissionsSection shows Full Disk Access on macOS, with the guide the 维护 page uses. Windows has no such permission, so it shows nothing there. */
function PermissionsSection() {
  const queryClient = useQueryClient();
  const state = useQuery(fullDiskAccessQueryOptions);
  const guide = useMutation({
    mutationFn: () => Permissions.guideFullDiskAccess(),
    onSuccess: (next) => queryClient.setQueryData(fullDiskAccessQueryOptions.queryKey, next),
  });
  const ids = useRowIds();
  if (isWindows() || state.data === "notRequired") return null;
  const text = state.data ? PERMISSION_TEXT[state.data] : state.isError ? "无法确定" : "正在检查…";
  return (
    <Section
      title="权限"
      tone="amber"
      icon={
        <>
          <rect x="5" y="11" width="14" height="9" rx="2" />
          <path d="M8 11V8a4 4 0 0 1 8 0v3" />
        </>
      }
    >
      <SettingRow
        label="完全磁盘访问权限"
        description={`优化 Safari 和邮件的数据库、读取${trashName()}和查找部分应用的残留文件时需要这项权限。`}
        error={guide.isError ? "无法打开系统设置" : null}
        {...ids}
      >
        <span
          className={state.data === "granted" ? "permission-state permission-state--ok" : "permission-state"}
        >
          {text}
        </span>
        {state.data !== undefined && state.data !== "granted" && (
          <button
            type="button"
            className="ghost-button"
            aria-describedby={ids.descriptionId}
            disabled={guide.isPending}
            onClick={() => guide.mutate()}
          >
            {guide.isPending ? "等待授权…" : "去授权"}
          </button>
        )}
      </SettingRow>
    </Section>
  );
}

function AboutSection() {
  const about = useQuery(aboutQueryOptions);
  const reveal = useReveal();
  const version = useRowIds();
  const data = useRowIds();
  const licenses = useRowIds();
  const dataDir = about.data?.dataDir;
  return (
    <Section
      title="关于"
      tone="violet"
      icon={
        <>
          <circle cx="12" cy="12" r="9" />
          <path d="M12 11v5M12 8h.01" />
        </>
      }
    >
      <SettingRow label="Spruce" description="清理磁盘、保养系统的桌面工具。" {...version}>
        <span className="about-value">
          {about.data ? `版本 ${about.data.version || "开发版"}` : about.isError ? "版本未知" : ""}
        </span>
      </SettingRow>
      <SettingRow
        label="数据文件夹"
        description={dataDir ?? (about.isError ? errorMessage(about.error) : "正在读取…")}
        error={reveal.isError ? errorMessage(reveal.error) : null}
        {...data}
      >
        <button
          type="button"
          className="ghost-button"
          aria-describedby={data.descriptionId}
          disabled={!dataDir}
          onClick={() => dataDir && reveal.mutate(dataDir)}
        >
          {`在${fileManagerName()}中显示`}
        </button>
      </SettingRow>
      <SettingRow label="开源许可" description="Spruce 使用了以下开源软件：" stacked {...licenses}>
        <ul className="license-list" aria-labelledby={licenses.labelId}>
          {LICENSES.map((l) => (
            <li key={l.name}>
              <span>{l.name}</span>
              <span className="license-name">{l.license}</span>
            </li>
          ))}
        </ul>
      </SettingRow>
    </Section>
  );
}

function ResetDialog({ onDone, onCancel }: { onDone: () => void; onCancel: () => void }) {
  const reset = useResetSettings();
  return (
    <ConfirmDialog
      title="恢复默认设置？"
      message={
        <>
          所有设置都会回到初始值，「登录时启动」会关闭。操作历史不受影响。
          {reset.isError && (
            <span role="alert" className="settings-confirm-error">
              {errorMessage(reset.error)}
            </span>
          )}
        </>
      }
      confirmLabel="恢复默认"
      tone="danger"
      busy={reset.isPending}
      onConfirm={() => reset.mutate(undefined, { onSuccess: onDone })}
      onCancel={onCancel}
    />
  );
}
