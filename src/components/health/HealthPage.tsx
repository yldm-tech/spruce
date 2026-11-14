import { useMutation, useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { useMemo } from "react";

import { Health, type Vitals } from "../../mygo";
import { isWindows, systemMonitorName } from "../../platform";
import { Page, PageHeader } from "../common/Page";
import { PAGES } from "../common/pages";
import { useSettings } from "../common/settings";
import { AlertBanner } from "../maintenance/AlertBanner";
import { BatteryCard, CPUCard, DiskCard, MemoryCard, NetworkCard, SystemCard } from "./cards";
import { chartData } from "./format";
import { ProcessTable } from "./ProcessTable";
import { systemInfoQueryOptions, useVitals } from "./vitals";

import "../maintenance/maintenance.css";
import "./health.css";

const pickCPU = [(v: Vitals) => v.cpu?.percent] as const;
const pickMemory = [(v: Vitals) => v.memory?.percent] as const;
const pickDisk = [(v: Vitals) => v.diskIo?.readPerSec, (v: Vitals) => v.diskIo?.writePerSec] as const;
const pickNetwork = [
  (v: Vitals) => v.network?.downloadPerSec,
  (v: Vitals) => v.network?.uploadPerSec,
] as const;

/** AlertSettingsCard states the current high-CPU alert settings, which 设置 changes. */
function AlertSettingsCard() {
  const settings = useSettings();
  const health = settings.data?.health;
  let text = "正在读取提醒设置…";
  if (settings.isError) text = "无法读取提醒设置";
  else if (health && !health.enabled) text = "高 CPU 提醒已关闭";
  else if (health)
    text = `高 CPU 提醒：单个进程持续 ${health.sustainMinutes} 分钟占用超过 ${health.cpuThreshold}% CPU 时提醒`;
  return (
    <section aria-label="高 CPU 提醒设置" className="alert-settings">
      <span className="alert-settings-text">{text}</span>
      <Link to="/settings" className="alert-settings-link">
        {health && !health.enabled ? "去设置开启" : "调整阈值"}
      </Link>
    </section>
  );
}

/** HealthPage is the 健康监测 page: the CPU alert (as on 维护), the alert settings, live overview cards with two-minute sparklines, and the processes using the most CPU and memory. It only reads: Spruce never ends processes. */
export function HealthPage() {
  const { samples, failed, retry } = useVitals();
  const info = useQuery(systemInfoQueryOptions);
  const openMonitor = useMutation({ mutationFn: () => Health.openSystemMonitor() });
  const monitor = systemMonitorName();

  const latest = samples.at(-1);
  const charts = useMemo(
    () => ({
      cpu: chartData(samples, pickCPU),
      memory: chartData(samples, pickMemory),
      disk: chartData(samples, pickDisk),
      network: chartData(samples, pickNetwork),
    }),
    [samples],
  );
  // Uptime is measured to the latest sample, so it moves with the samples.
  const now = latest ? Date.parse(latest.at) : undefined;
  // A desktop without a battery has no card; one whose battery could not be read says so.
  const showBattery = latest !== undefined && (latest.battery === null || latest.battery.present);
  const processes = latest ? latest.processes : undefined;

  return (
    <Page className="health-page">
      <PageHeader {...PAGES.health} />

      <AlertBanner />
      <AlertSettingsCard />

      {failed && (
        <div role="alert" className="health-error">
          <span>无法读取系统状态</span>
          <button type="button" className="alert-banner-button" onClick={retry}>
            重试
          </button>
        </div>
      )}

      <div className="vitals-grid">
        <CPUCard latest={latest} info={info.data} chart={charts.cpu} />
        <MemoryCard latest={latest} chart={charts.memory} />
        <DiskCard latest={latest} chart={charts.disk} />
        <NetworkCard latest={latest} chart={charts.network} />
        {showBattery && <BatteryCard battery={latest.battery} />}
        <SystemCard info={info.data} failed={info.isError} now={now} />
      </div>

      <section aria-labelledby="health-processes-title" className="process-section">
        <div className="process-section-head">
          <h2 id="health-processes-title" className="process-section-title">
            进程
          </h2>
          <button type="button" className="ghost-button" onClick={() => openMonitor.mutate()}>
            {`在${monitor.name}中打开`}
          </button>
        </div>
        {openMonitor.isError && (
          <span role="alert" className="process-section-error">
            {`无法打开${monitor.name}`}
          </span>
        )}
        <div className="process-grid">
          <ProcessTable title="CPU 占用最高" processes={processes && processes.topCpu} sortedBy="cpu" />
          <ProcessTable title="内存占用最高" processes={processes && processes.topMemory} sortedBy="memory" />
        </div>
        <p className="process-footnote">
          {isWindows()
            ? "CPU 按单个处理器核心计算，100% 即占满一个核心；任务管理器按全部核心显示，数值会更低。受保护的系统进程读不到 CPU 占用。"
            : "CPU 按单个处理器核心计算，100% 即占满一个核心，与活动监视器一致。其他用户和系统的进程（如 WindowServer）无法读取，不在列表中。"}
          Spruce 只显示进程，不会结束进程。
        </p>
      </section>
    </Page>
  );
}
